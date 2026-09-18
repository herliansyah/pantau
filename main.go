package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strconv"
	"syscall"
	"time"

	"github.com/mattn/go-isatty"

	"pantau/internal/inspector"
	"pantau/internal/notify"
	"pantau/internal/snapshot"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
	"pantau/internal/updater"
	"pantau/internal/web"
)

//go:generate go run github.com/tc-hib/go-winres@v0.3.3 make --arch amd64,arm64

//go:embed README.md README.id.md docs/user-guide.md docs/user-guide.id.md
var embeddedDocs embed.FS

var Version = "dev"

func resolveVersion() string {
	if Version != "" && Version != "dev" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		var rev, mod string
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.modified":
				if s.Value == "true" {
					mod = "-dirty"
				}
			}
		}
		if rev != "" {
			if len(rev) > 7 {
				rev = rev[:7]
			}
			return rev + mod
		}
	}
	return "dev"
}

func printBanner(version string) {
	isTTY := isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
	if os.Getenv("NO_COLOR") != "" {
		isTTY = false
	}

	cyan := ""
	dim := ""
	reset := ""
	if isTTY {
		cyan = "\033[36m"
		dim = "\033[2m"
		reset = "\033[0m"
	}

	fmt.Printf(`%s  ____              _             
 |  _ \ __ _ _ __ | |_ __ _ _   _ 
 | |_) / _`+"`"+` | '_ \| __/ _`+"`"+` | | | |
 |  __/ (_| | | | | || (_| | |_| |
 |_|   \__,_|_| |_|\__\__,_|\__,_|%s
 %sSingle binary. Zero remote daemons. Embedded SQLite. Native SSH.%s

 • %sVersion%s : %s
 • %sAuthor%s  : Herliansyah
 • %sGitHub%s  : https://github.com/herliansyah/pantau

`, cyan, reset, dim, reset, dim, reset, version, dim, reset, dim, reset)
}

func resolveConfig(flagPort int, flagDB string) (port int, dbPath string, portExplicit bool) {
	port = flagPort
	if envPort := os.Getenv("PANTAU_PORT"); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil {
			port = p
			portExplicit = true
		}
	} else if envPort := os.Getenv("PORT"); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil {
			port = p
			portExplicit = true
		}
	}

	dbPath = flagDB
	if envDB := os.Getenv("PANTAU_DB"); envDB != "" {
		dbPath = envDB
	} else if envDB := os.Getenv("DB_PATH"); envDB != "" {
		dbPath = envDB
	}
	return port, dbPath, portExplicit
}

func main() {
	updater.CleanOldArtifacts()

	versionFlag := flag.Bool("version", false, "Print version and exit")
	flag.BoolVar(versionFlag, "v", false, "Print version and exit (shorthand)")
	checkUpdateFlag := flag.Bool("check-update", false, "Check for newer Pantau release and exit")
	updateFlag := flag.Bool("update", false, "Self-update Pantau to latest release and exit")
	disableUpdateCheckFlag := flag.Bool("disable-update-check", false, "Disable background update checks (recommended for airgapped environments)")
	portFlag := flag.Int("port", 8080, "HTTP server port")
	dbFlag := flag.String("db", "pantau.db", "SQLite database file path")
	openBrowserFlag := flag.Bool("open", runtime.GOOS == "windows", "Open default browser on start")
	disable2FAFlag := flag.Bool("disable-2fa", false, "Emergency bypass flag to disable 2FA")
	flag.Parse()

	ver := resolveVersion()
	if *versionFlag {
		printBanner(ver)
		os.Exit(0)
	}

	if *checkUpdateFlag {
		printBanner(ver)
		mgr := updater.NewManager(ver, false)
		fmt.Printf("🔍 Checking for updates (current version: %s)...\n", ver)
		res, err := mgr.Check(context.Background(), true)
		if err != nil || (res != nil && res.Error != "") {
			errMsg := ""
			if res != nil && res.Error != "" {
				errMsg = res.Error
			} else if err != nil {
				errMsg = err.Error()
			}
			fmt.Printf("⚠️ Update check warning: %s\n", errMsg)
			os.Exit(0)
		}
		if res.Available {
			fmt.Printf("✨ New version available: %s\n", res.LatestVersion)
			fmt.Printf("🔗 Release URL: %s\n", res.ReleaseURL)
			fmt.Println("👉 Run 'pantau -update' to install the new version.")
		} else {
			fmt.Printf("✅ Pantau is up to date (%s).\n", ver)
		}
		os.Exit(0)
	}

	if *updateFlag {
		printBanner(ver)
		mgr := updater.NewManager(ver, false)
		fmt.Printf("🚀 Updating Pantau from %s to latest release...\n", ver)
		if err := mgr.ApplyUpdate(context.Background()); err != nil {
			fmt.Printf("❌ Update failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✅ Update completed successfully! Please restart Pantau.")
		os.Exit(0)
	}

	printBanner(ver)
	portExplicit := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "port" {
			portExplicit = true
		}
	})

	port, dbPath, envPortExplicit := resolveConfig(*portFlag, *dbFlag)
	if envPortExplicit {
		portExplicit = true
	}

	log.Printf("Starting Pantau %s...", ver)
	log.Printf("Database path: %s", dbPath)

	db, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	if *disable2FAFlag {
		_ = db.SetSetting("totp_enabled", "false")
		_ = db.DeleteSetting("totp_secret")
		_ = db.DeleteSetting("totp_recovery_codes")
		_ = db.DeleteSetting("last_totp_step")
		log.Println("[SECURITY] 2FA has been disabled via -disable-2fa CLI bypass flag.")
	}

	runnerFactory := sshrunner.DefaultFactory()
	dispatcher := notify.New(db)
	ins := inspector.New(db, runnerFactory, dispatcher)

	// Start background inspection worker
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go startInspectionWorker(ctx, db, ins)
	snapshotMgr := snapshot.NewManager(db, nil)
	go snapshotMgr.Start(ctx)

	disableUpdateCheck := *disableUpdateCheckFlag || os.Getenv("PANTAU_DISABLE_UPDATE_CHECK") == "true"
	updaterMgr := updater.NewManager(ver, disableUpdateCheck)
	updaterMgr.StartBackgroundTicker(ctx)

	server := web.NewServer(db, ins, dispatcher)
	server.SetVersion(ver)
	server.SetSnapshotManager(snapshotMgr)
	server.SetUpdaterManager(updaterMgr)
	server.SetDocsFS(embeddedDocs)
	var listener net.Listener
	if !portExplicit {
		for p := port; p <= port+19; p++ {
			l, err := net.Listen("tcp", fmt.Sprintf(":%d", p))
			if err == nil {
				listener = l
				if p != port {
					log.Printf("⚠️ Port %d was in use, auto-scanned and bound to port %d", port, p)
				}
				port = p
				break
			}
		}
		if listener == nil {
			log.Fatalf("Failed to bind port: ports %d-%d are all in use", port, port+19)
		}
	} else {
		l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err != nil {
			log.Fatalf("Failed to bind port %d: %v", port, err)
		}
		listener = l
	}

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: server,
	}

	updaterMgr.SetShutdownFunc(func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer shutdownCancel()
		_ = httpServer.Shutdown(shutdownCtx)
	})

	go func() {
		log.Printf("🛡️ Pantau running on http://0.0.0.0:%d (Default password: admin)", port)
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()
	if *openBrowserFlag {
		go func() {
			time.Sleep(150 * time.Millisecond)
			openBrowser(fmt.Sprintf("http://localhost:%d", port))
		}()
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down Pantau...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = httpServer.Shutdown(shutdownCtx)
	log.Println("Pantau stopped cleanly.")
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

func startInspectionWorker(ctx context.Context, db *store.DB, ins *inspector.Inspector) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hosts, err := db.ListHosts()
			if err != nil {
				continue
			}
			interval := 300 * time.Second
			if pollStr, err := db.GetSetting("poll_interval_sec"); err == nil && pollStr != "" {
				if sec, err := strconv.Atoi(pollStr); err == nil && sec > 0 {
					interval = time.Duration(sec) * time.Second
				}
			}

			now := time.Now()
			for _, h := range hosts {
				if (h.LastInspected == nil || now.Sub(*h.LastInspected) >= interval) && !ins.IsInspecting(h.ID) {
					go func(hostID int64) {
						_ = ins.InspectHost(hostID)
					}(h.ID)
				}
			}
		}
	}
}
