package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/notify"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
	"pantau/internal/web"
)

func main() {
	portFlag := flag.Int("port", 8080, "HTTP server port")
	dbFlag := flag.String("db", "data/pantau.db", "SQLite database file path")
	flag.Parse()

	port := *portFlag
	if envPort := os.Getenv("PANTAU_PORT"); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil {
			port = p
		}
	}

	dbPath := *dbFlag
	if envDB := os.Getenv("PANTAU_DB"); envDB != "" {
		dbPath = envDB
	}

	log.Printf("Starting Pantau...")
	log.Printf("Database path: %s", dbPath)

	db, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	runnerFactory := sshrunner.DefaultFactory()
	dispatcher := notify.New(db)
	ins := inspector.New(db, runnerFactory, dispatcher)

	// Start background inspection worker
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go startInspectionWorker(ctx, db, ins)

	server := web.NewServer(db, ins, dispatcher)
	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: server,
	}

	go func() {
		log.Printf("🛡️ Pantau running on http://0.0.0.0:%d (Default password: admin)", port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down Pantau...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = httpServer.Shutdown(shutdownCtx)
	log.Println("Pantau stopped cleanly.")
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
				if h.LastInspected == nil || now.Sub(*h.LastInspected) >= interval {
					go func(hostID int64) {
						_ = ins.InspectHost(hostID)
					}(h.ID)
				}
			}
		}
	}
}
