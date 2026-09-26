package store

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/ssh"
	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
}

type Host struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	Host           string    `json:"host"`
	Port           int       `json:"port"`
	User           string    `json:"user"`
	CustomKey      string    `json:"custom_key,omitempty"`
	Status         string    `json:"status"` // healthy, degraded, down, unknown
	OSInfo         string    `json:"os_info"`
	Kernel         string    `json:"kernel"`
	Uptime         string    `json:"uptime"`
	CPULoad        string    `json:"cpu_load"`
	RAMUsedBytes   int64     `json:"ram_used_bytes"`
	RAMTotalBytes  int64     `json:"ram_total_bytes"`
	DiskUsedBytes     int64      `json:"disk_used_bytes"`
	DiskTotalBytes    int64      `json:"disk_total_bytes"`
	CPUCores          int        `json:"cpu_cores"`
	HardwareModel     string     `json:"hardware_model"`
	SwapUsedBytes     int64      `json:"swap_used_bytes"`
	SwapTotalBytes    int64      `json:"swap_total_bytes"`
	LifecycleScore    int        `json:"lifecycle_score"`
	LifecycleNotes    string     `json:"lifecycle_notes"`
	LifecycleBreakdown string    `json:"lifecycle_breakdown"`
	LastInspected     *time.Time `json:"last_inspected"`
	CreatedAt         time.Time  `json:"created_at"`

	// Network & Security Observability
	NetRxBytes        int64  `json:"net_rx_bytes"`
	NetTxBytes        int64  `json:"net_tx_bytes"`
	NetRxSpeedBps     int64  `json:"net_rx_speed_bps"`
	NetTxSpeedBps     int64  `json:"net_tx_speed_bps"`
	InternetOnline    bool   `json:"internet_online"`
	InternetLatencyMs int64  `json:"internet_latency_ms"`
	PublicIP          string `json:"public_ip"`
	ActiveConnCount   int    `json:"active_conn_count"`
	FailedLoginsCount int    `json:"failed_logins_count"`
	TopConnections    string `json:"top_connections"` // JSON serialized array of TopConn
	ListeningPorts    string `json:"listening_ports"` // JSON serialized array of ListeningPort
	Notes             string `json:"notes"`
	SortOrder         int    `json:"sort_order"`
	GroupName         string `json:"group_name"`
	LastDurationMs    int64  `json:"last_duration_ms"`
	CommissionDate    string `json:"commission_date"` // YYYY-MM-DD manual override for New Old Stock
	BIOSDate          string `json:"bios_date"`       // Raw motherboard BIOS release date telemetry
	OSInstallEpoch    int64  `json:"os_install_epoch"` // Unix epoch of OS deployment for VM age tracking
}

type TopConn struct {
	RemoteIP string `json:"remote_ip"`
	Port     string `json:"port"`
	Count    int    `json:"count"`
}

type ListeningPort struct {
	Proto   string `json:"proto"`
	Port    string `json:"port"`
	Address string `json:"address"`
	Public  bool   `json:"public"`
	Process string `json:"process"`
	Risk    string `json:"risk"` // "low", "high"
}

type DesiredRule struct {
	ID       int64  `json:"id"`
	HostID   int64  `json:"host_id"`
	Kind     string `json:"kind"`     // container, disk, backup, cron, service
	Target   string `json:"target"`   // e.g. "nginx", "/", "/var/backups/db.sql.gz", "mysql"
	Expected string `json:"expected"` // e.g. "running", "<85%", "fresh_24h", "active"
	Enabled  bool   `json:"enabled"`
}

type ActualItem struct {
	ID        int64     `json:"id"`
	HostID    int64     `json:"host_id"`
	Kind      string    `json:"kind"`
	Target    string    `json:"target"`
	Current   string    `json:"current"`
	Status    string    `json:"status"` // ok, drift
	Details   string    `json:"details"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Incident struct {
	ID               int64     `json:"id"`
	HostID           int64     `json:"host_id"`
	Kind             string    `json:"kind"`
	Target           string    `json:"target"`
	EventType        string    `json:"event_type"` // drift_detected, drift_resolved
	Summary          string    `json:"summary"`
	RootCauseExcerpt string    `json:"root_cause_excerpt"`
	CreatedAt        time.Time `json:"created_at"`
}

type Alert struct {
	ID           int64     `json:"id"`
	HostID       int64     `json:"host_id"`
	HostName     string    `json:"host_name"`
	Level        string    `json:"level"` // critical, warning, info
	Message      string    `json:"message"`
	RootCause    string    `json:"root_cause"`
	Acknowledged bool      `json:"acknowledged"`
	CreatedAt    time.Time `json:"created_at"`
}

type TerminalPreset struct {
	ID        int64     `json:"id"`
	HostID    *int64    `json:"host_id"` // nil indicates a global preset
	Name      string    `json:"name"`
	Command   string    `json:"command"`
	SortOrder int       `json:"sort_order"`
	UseTmux   bool      `json:"use_tmux"`
	CreatedAt time.Time `json:"created_at"`
}

type InspectionRun struct {
	ID         int64     `json:"id"`
	HostID     int64     `json:"host_id"`
	StartedAt  time.Time `json:"started_at"`
	DurationMs int64     `json:"duration_ms"`
	Status     string    `json:"status"` // ok, drift, degraded, down, error
	Summary    string    `json:"summary"`
	Details    string    `json:"details"`
	CreatedAt  time.Time `json:"created_at"`
}

func Open(dbPath string) (*DB, error) {
	if dir := filepath.Dir(dbPath); dir != "." && dir != "" {
		_ = os.MkdirAll(dir, 0755)
	}

	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	s := &DB{db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	if err := s.initDefaults(); err != nil {
		db.Close()
		return nil, fmt.Errorf("init defaults: %w", err)
	}

	return s, nil
}

func (d *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS hosts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		host TEXT NOT NULL,
		port INTEGER NOT NULL DEFAULT 22,
		user TEXT NOT NULL DEFAULT 'root',
		custom_key TEXT,
		status TEXT DEFAULT 'unknown',
		os_info TEXT DEFAULT '',
		kernel TEXT DEFAULT '',
		uptime TEXT DEFAULT '',
		cpu_load TEXT DEFAULT '',
		ram_used_bytes INTEGER DEFAULT 0,
		ram_total_bytes INTEGER DEFAULT 0,
		disk_used_bytes INTEGER DEFAULT 0,
		disk_total_bytes INTEGER DEFAULT 0,
		cpu_cores INTEGER DEFAULT 1,
		hardware_model TEXT DEFAULT '',
		swap_used_bytes INTEGER DEFAULT 0,
		swap_total_bytes INTEGER DEFAULT 0,
		lifecycle_score INTEGER DEFAULT 100,
		lifecycle_notes TEXT DEFAULT '',
		lifecycle_breakdown TEXT DEFAULT '[]',
		last_inspected DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		net_rx_bytes INTEGER DEFAULT 0,
		net_tx_bytes INTEGER DEFAULT 0,
		net_rx_speed_bps INTEGER DEFAULT 0,
		net_tx_speed_bps INTEGER DEFAULT 0,
		internet_online INTEGER DEFAULT 0,
		internet_latency_ms INTEGER DEFAULT 0,
		public_ip TEXT DEFAULT '',
		active_conn_count INTEGER DEFAULT 0,
		failed_logins_count INTEGER DEFAULT 0,
		top_connections TEXT DEFAULT '',
		listening_ports TEXT DEFAULT '',
		notes TEXT DEFAULT '',
		sort_order INTEGER DEFAULT 0,
		group_name TEXT DEFAULT '',
		commission_date TEXT DEFAULT '',
		bios_date TEXT DEFAULT '',
		os_install_epoch INTEGER DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS desired_rules (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id INTEGER NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
		kind TEXT NOT NULL,
		target TEXT NOT NULL,
		expected TEXT NOT NULL,
		enabled INTEGER DEFAULT 1,
		UNIQUE(host_id, kind, target)
	);

	CREATE TABLE IF NOT EXISTS actual_items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id INTEGER NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
		kind TEXT NOT NULL,
		target TEXT NOT NULL,
		current TEXT NOT NULL,
		status TEXT NOT NULL,
		details TEXT DEFAULT '',
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(host_id, kind, target)
	);

	CREATE TABLE IF NOT EXISTS incidents (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id INTEGER NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
		kind TEXT NOT NULL,
		target TEXT NOT NULL,
		event_type TEXT NOT NULL,
		summary TEXT NOT NULL,
		root_cause_excerpt TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS alerts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id INTEGER REFERENCES hosts(id) ON DELETE CASCADE,
		level TEXT NOT NULL,
		message TEXT NOT NULL,
		root_cause TEXT DEFAULT '',
		acknowledged INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS terminal_presets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id INTEGER REFERENCES hosts(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		command TEXT NOT NULL,
		sort_order INTEGER DEFAULT 0,
		use_tmux INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS inspection_runs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id INTEGER NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
		started_at DATETIME NOT NULL,
		duration_ms INTEGER NOT NULL,
		status TEXT NOT NULL,
		summary TEXT NOT NULL,
		details TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_inspection_runs_host ON inspection_runs(host_id, id DESC);
	CREATE INDEX IF NOT EXISTS idx_inspection_runs_status ON inspection_runs(host_id, status, id DESC);
	`
	_, err := d.Exec(schema)
	if err != nil {
		return err
	}

	// Idempotent column additions for existing databases
	_ = d.alterAddColumn("hosts", "net_rx_bytes", "INTEGER DEFAULT 0")
	_ = d.alterAddColumn("hosts", "net_tx_bytes", "INTEGER DEFAULT 0")
	_ = d.alterAddColumn("hosts", "net_rx_speed_bps", "INTEGER DEFAULT 0")
	_ = d.alterAddColumn("hosts", "net_tx_speed_bps", "INTEGER DEFAULT 0")
	_ = d.alterAddColumn("hosts", "internet_online", "INTEGER DEFAULT 0")
	_ = d.alterAddColumn("hosts", "internet_latency_ms", "INTEGER DEFAULT 0")
	_ = d.alterAddColumn("hosts", "public_ip", "TEXT DEFAULT ''")
	_ = d.alterAddColumn("hosts", "active_conn_count", "INTEGER DEFAULT 0")
	_ = d.alterAddColumn("hosts", "failed_logins_count", "INTEGER DEFAULT 0")
	_ = d.alterAddColumn("hosts", "top_connections", "TEXT DEFAULT ''")
	_ = d.alterAddColumn("hosts", "listening_ports", "TEXT DEFAULT ''")
	_ = d.alterAddColumn("hosts", "notes", "TEXT DEFAULT ''")
	_ = d.alterAddColumn("hosts", "sort_order", "INTEGER DEFAULT 0")
	_ = d.alterAddColumn("hosts", "group_name", "TEXT DEFAULT ''")
	_ = d.alterAddColumn("hosts", "lifecycle_breakdown", "TEXT DEFAULT '[]'")
	_ = d.alterAddColumn("hosts", "cpu_cores", "INTEGER DEFAULT 1")
	_ = d.alterAddColumn("hosts", "hardware_model", "TEXT DEFAULT ''")
	_ = d.alterAddColumn("hosts", "swap_used_bytes", "INTEGER DEFAULT 0")
	_ = d.alterAddColumn("hosts", "swap_total_bytes", "INTEGER DEFAULT 0")
	_ = d.alterAddColumn("hosts", "last_duration_ms", "INTEGER DEFAULT 0")
	_ = d.alterAddColumn("hosts", "commission_date", "TEXT DEFAULT ''")
	_ = d.alterAddColumn("hosts", "bios_date", "TEXT DEFAULT ''")
	_ = d.alterAddColumn("hosts", "os_install_epoch", "INTEGER DEFAULT 0")

	return nil
}

func (d *DB) alterAddColumn(table, column, colType string) error {
	_, err := d.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, colType))
	return err
}

func (d *DB) initDefaults() error {
	// 1. Ensure default admin password exists (default: "admin")
	var hash string
	err := d.QueryRow("SELECT value FROM settings WHERE key = 'admin_password_hash'").Scan(&hash)
	if err == sql.ErrNoRows {
		defaultHash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		if _, err := d.Exec("INSERT INTO settings(key, value) VALUES ('admin_password_hash', ?)", string(defaultHash)); err != nil {
			return err
		}
	}

	// 2. Ensure global universal RSA 4096-bit SSH keypair exists
	// ponytail: RSA 4096 is universally accepted by legacy OpenSSH 5.3p1 (CentOS 6) through modern OpenSSH 9+.
	var privKey string
	err = d.QueryRow("SELECT value FROM settings WHERE key = 'ssh_private_key'").Scan(&privKey)
	if err == sql.ErrNoRows {
		privPEM, pubAuthorized, err := GenerateRSAKeyPair(4096)
		if err != nil {
			return fmt.Errorf("generate default universal rsa key: %w", err)
		}

		tx, err := d.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		if _, err := tx.Exec("INSERT INTO settings(key, value) VALUES ('ssh_private_key', ?)", privPEM); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT INTO settings(key, value) VALUES ('ssh_public_key', ?)", pubAuthorized); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	} else if err == nil {
		var pubKey string
		_ = d.QueryRow("SELECT value FROM settings WHERE key = 'ssh_public_key'").Scan(&pubKey)
		// If existing database was initialized with Ed25519 only, upgrade to universal RSA-4096 as primary
		// while retaining the Ed25519 private key in the PEM chain so existing modern hosts don't break.
		if strings.HasPrefix(strings.TrimSpace(pubKey), "ssh-ed25519") {
			privPEM, pubAuthorized, err := GenerateRSAKeyPair(4096)
			if err == nil {
				combinedPriv := strings.TrimSpace(privPEM) + "\n\n" + strings.TrimSpace(privKey) + "\n"
				tx, err := d.Begin()
				if err == nil {
					_, _ = tx.Exec("UPDATE settings SET value = ? WHERE key = 'ssh_private_key'", combinedPriv)
					_, _ = tx.Exec("UPDATE settings SET value = ? WHERE key = 'ssh_public_key'", pubAuthorized)
					_, _ = tx.Exec("INSERT INTO settings(key, value) VALUES ('ssh_private_key_ed25519', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", privKey)
					_, _ = tx.Exec("INSERT INTO settings(key, value) VALUES ('ssh_public_key_ed25519', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", pubKey)
					_ = tx.Commit()
				}
			}
		}
	}

	// 3. Ensure essential default global terminal presets exist
	var presetCount int
	_ = d.QueryRow("SELECT COUNT(*) FROM terminal_presets").Scan(&presetCount)
	if presetCount == 0 {
		defaultPresets := []struct {
			name    string
			command string
			sort    int
		}{
			{"htop", "htop", 1},
			{"docker stats", "docker stats", 2},
			{"journalctl -f", "journalctl -n 100 -f", 3},
		}
		for _, p := range defaultPresets {
			_, _ = d.Exec("INSERT INTO terminal_presets(name, command, sort_order, use_tmux) VALUES (?, ?, ?, 0)", p.name, p.command, p.sort)
		}
	}

	return nil
}

// GenerateRSAKeyPair creates a new PEM-encoded RSA private key and OpenSSH-formatted authorized public key.
func GenerateRSAKeyPair(bits int) (privPEM, pubAuthorized string, err error) {
	if bits <= 0 {
		bits = 4096
	}
	priv, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return "", "", fmt.Errorf("generate rsa key: %w", err)
	}

	privBytes := x509.MarshalPKCS1PrivateKey(priv)
	pemBlock := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privBytes,
	})

	sshPub, err := ssh.NewPublicKey(&priv.PublicKey)
	if err != nil {
		return "", "", fmt.Errorf("new ssh pubkey: %w", err)
	}
	pubAuth := string(ssh.MarshalAuthorizedKey(sshPub))
	return string(pemBlock), pubAuth, nil
}

// RegenerateGlobalSSHKey generates a new universal RSA-4096 keypair and saves it to settings.
func (d *DB) RegenerateGlobalSSHKey() (string, error) {
	privPEM, pubAuthorized, err := GenerateRSAKeyPair(4096)
	if err != nil {
		return "", err
	}
	tx, err := d.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("INSERT INTO settings(key, value) VALUES ('ssh_private_key', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", privPEM); err != nil {
		return "", err
	}
	if _, err := tx.Exec("INSERT INTO settings(key, value) VALUES ('ssh_public_key', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", pubAuthorized); err != nil {
		return "", err
	}
	return pubAuthorized, tx.Commit()
}

// Settings helpers
func (d *DB) GetSetting(key string) (string, error) {
	var val string
	err := d.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&val)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return val, err
}

func (d *DB) SetSetting(key, val string) error {
	_, err := d.Exec("INSERT INTO settings(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", key, val)
	return err
}

func (d *DB) DeleteSetting(key string) error {
	_, err := d.Exec("DELETE FROM settings WHERE key = ?", key)
	return err
}


// Host operations
func (d *DB) ListHosts() ([]Host, error) {
	rows, err := d.Query(`SELECT id, name, host, port, user, COALESCE(custom_key,''), status, os_info, kernel, uptime, cpu_load, ram_used_bytes, ram_total_bytes, disk_used_bytes, disk_total_bytes, lifecycle_score, lifecycle_notes, COALESCE(lifecycle_breakdown, '[]'), last_inspected, created_at,
		COALESCE(net_rx_bytes, 0), COALESCE(net_tx_bytes, 0), COALESCE(net_rx_speed_bps, 0), COALESCE(net_tx_speed_bps, 0),
		COALESCE(internet_online, 0), COALESCE(internet_latency_ms, 0), COALESCE(public_ip, ''), COALESCE(active_conn_count, 0),
		COALESCE(failed_logins_count, 0), COALESCE(top_connections, ''), COALESCE(listening_ports, ''),
		COALESCE(notes, ''), COALESCE(sort_order, 0), COALESCE(group_name, ''),
		COALESCE(cpu_cores, 1), COALESCE(hardware_model, ''), COALESCE(swap_used_bytes, 0), COALESCE(swap_total_bytes, 0),
		COALESCE(last_duration_ms, 0), COALESCE(commission_date, ''), COALESCE(bios_date, ''), COALESCE(os_install_epoch, 0)
		FROM hosts ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Host
	for rows.Next() {
		var h Host
		var inspected sql.NullTime
		var online int
		if err := rows.Scan(&h.ID, &h.Name, &h.Host, &h.Port, &h.User, &h.CustomKey, &h.Status, &h.OSInfo, &h.Kernel, &h.Uptime, &h.CPULoad, &h.RAMUsedBytes, &h.RAMTotalBytes, &h.DiskUsedBytes, &h.DiskTotalBytes, &h.LifecycleScore, &h.LifecycleNotes, &h.LifecycleBreakdown, &inspected, &h.CreatedAt,
			&h.NetRxBytes, &h.NetTxBytes, &h.NetRxSpeedBps, &h.NetTxSpeedBps,
			&online, &h.InternetLatencyMs, &h.PublicIP, &h.ActiveConnCount,
			&h.FailedLoginsCount, &h.TopConnections, &h.ListeningPorts,
			&h.Notes, &h.SortOrder, &h.GroupName,
			&h.CPUCores, &h.HardwareModel, &h.SwapUsedBytes, &h.SwapTotalBytes,
			&h.LastDurationMs, &h.CommissionDate, &h.BIOSDate, &h.OSInstallEpoch); err != nil {
			return nil, err
		}
		h.InternetOnline = online == 1
		if inspected.Valid {
			h.LastInspected = &inspected.Time
		}
		list = append(list, h)
	}
	return list, nil
}

func (d *DB) GetHost(id int64) (*Host, error) {
	var h Host
	var inspected sql.NullTime
	var online int
	err := d.QueryRow(`SELECT id, name, host, port, user, COALESCE(custom_key,''), status, os_info, kernel, uptime, cpu_load, ram_used_bytes, ram_total_bytes, disk_used_bytes, disk_total_bytes, lifecycle_score, lifecycle_notes, COALESCE(lifecycle_breakdown, '[]'), last_inspected, created_at,
		COALESCE(net_rx_bytes, 0), COALESCE(net_tx_bytes, 0), COALESCE(net_rx_speed_bps, 0), COALESCE(net_tx_speed_bps, 0),
		COALESCE(internet_online, 0), COALESCE(internet_latency_ms, 0), COALESCE(public_ip, ''), COALESCE(active_conn_count, 0),
		COALESCE(failed_logins_count, 0), COALESCE(top_connections, ''), COALESCE(listening_ports, ''),
		COALESCE(notes, ''), COALESCE(sort_order, 0), COALESCE(group_name, ''),
		COALESCE(cpu_cores, 1), COALESCE(hardware_model, ''), COALESCE(swap_used_bytes, 0), COALESCE(swap_total_bytes, 0),
		COALESCE(last_duration_ms, 0), COALESCE(commission_date, ''), COALESCE(bios_date, ''), COALESCE(os_install_epoch, 0)
		FROM hosts WHERE id = ?`, id).Scan(
		&h.ID, &h.Name, &h.Host, &h.Port, &h.User, &h.CustomKey, &h.Status, &h.OSInfo, &h.Kernel, &h.Uptime, &h.CPULoad, &h.RAMUsedBytes, &h.RAMTotalBytes, &h.DiskUsedBytes, &h.DiskTotalBytes, &h.LifecycleScore, &h.LifecycleNotes, &h.LifecycleBreakdown, &inspected, &h.CreatedAt,
		&h.NetRxBytes, &h.NetTxBytes, &h.NetRxSpeedBps, &h.NetTxSpeedBps,
		&online, &h.InternetLatencyMs, &h.PublicIP, &h.ActiveConnCount,
		&h.FailedLoginsCount, &h.TopConnections, &h.ListeningPorts,
		&h.Notes, &h.SortOrder, &h.GroupName,
		&h.CPUCores, &h.HardwareModel, &h.SwapUsedBytes, &h.SwapTotalBytes,
		&h.LastDurationMs, &h.CommissionDate, &h.BIOSDate, &h.OSInstallEpoch,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	h.InternetOnline = online == 1
	if inspected.Valid {
		h.LastInspected = &inspected.Time
	}
	return &h, nil
}

func (d *DB) CreateHost(h *Host) (int64, error) {
	var maxOrder int
	_ = d.QueryRow(`SELECT COALESCE(MAX(sort_order), 0) FROM hosts`).Scan(&maxOrder)
	res, err := d.Exec(`INSERT INTO hosts (name, host, port, user, custom_key, status, notes, sort_order, group_name, commission_date, bios_date) VALUES (?, ?, ?, ?, ?, 'unknown', ?, ?, ?, ?, ?)`, h.Name, h.Host, h.Port, h.User, h.CustomKey, h.Notes, maxOrder+1, h.GroupName, h.CommissionDate, h.BIOSDate)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) UpdateHost(h *Host) error {
	_, err := d.Exec(`UPDATE hosts SET name=?, host=?, port=?, user=?, custom_key=?, notes=?, group_name=?, commission_date=? WHERE id=?`, h.Name, h.Host, h.Port, h.User, h.CustomKey, h.Notes, h.GroupName, h.CommissionDate, h.ID)
	return err
}
func (d *DB) UpdateHostNotes(id int64, notes string) error {
	_, err := d.Exec(`UPDATE hosts SET notes=? WHERE id=?`, notes, id)
	return err
}

func (d *DB) UpdateHostLifecycle(id int64, score int, notes string, breakdown string) error {
	_, err := d.Exec(`UPDATE hosts SET lifecycle_score=?, lifecycle_notes=?, lifecycle_breakdown=? WHERE id=?`, score, notes, breakdown, id)
	return err
}

func (d *DB) ReorderHosts(orderedIDs []int64) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`UPDATE hosts SET sort_order=? WHERE id=?`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i, id := range orderedIDs {
		if _, err := stmt.Exec(i+1, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (d *DB) GetGlobalNotes() (string, error) {
	return d.GetSetting("global_notes")
}

func (d *DB) SetGlobalNotes(notes string) error {
	return d.SetSetting("global_notes", notes)
}

func (d *DB) DeleteHost(id int64) error {
	_, err := d.Exec(`DELETE FROM hosts WHERE id=?`, id)
	return err
}

func (d *DB) UpdateHostInspection(h *Host) error {
	now := time.Now()
	onlineInt := 0
	if h.InternetOnline {
		onlineInt = 1
	}
	_, err := d.Exec(`UPDATE hosts SET status=?, os_info=?, kernel=?, uptime=?, cpu_load=?, ram_used_bytes=?, ram_total_bytes=?, disk_used_bytes=?, disk_total_bytes=?, lifecycle_score=?, lifecycle_notes=?, lifecycle_breakdown=?, last_inspected=?, net_rx_bytes=?, net_tx_bytes=?, net_rx_speed_bps=?, net_tx_speed_bps=?, internet_online=?, internet_latency_ms=?, public_ip=?, active_conn_count=?, failed_logins_count=?, top_connections=?, listening_ports=?, cpu_cores=?, hardware_model=?, swap_used_bytes=?, swap_total_bytes=?, last_duration_ms=?, bios_date=?, os_install_epoch=? WHERE id=?`,
		h.Status, h.OSInfo, h.Kernel, h.Uptime, h.CPULoad, h.RAMUsedBytes, h.RAMTotalBytes, h.DiskUsedBytes, h.DiskTotalBytes, h.LifecycleScore, h.LifecycleNotes, h.LifecycleBreakdown, now,
		h.NetRxBytes, h.NetTxBytes, h.NetRxSpeedBps, h.NetTxSpeedBps, onlineInt, h.InternetLatencyMs, h.PublicIP, h.ActiveConnCount, h.FailedLoginsCount, h.TopConnections, h.ListeningPorts,
		h.CPUCores, h.HardwareModel, h.SwapUsedBytes, h.SwapTotalBytes, h.LastDurationMs, h.BIOSDate, h.OSInstallEpoch,
		h.ID)
	return err
}

// Inspection Run operations
func (d *DB) RecordInspectionRun(run *InspectionRun) error {
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now()
	}
	res, err := d.Exec(`INSERT INTO inspection_runs(host_id, started_at, duration_ms, status, summary, details) VALUES (?, ?, ?, ?, ?, ?)`,
		run.HostID, run.StartedAt, run.DurationMs, run.Status, run.Summary, run.Details)
	if err != nil {
		return err
	}
	run.ID, _ = res.LastInsertId()

	// ponytail: Dual-bucket auto-pruning preserves up to 100 latest OK runs and 100 latest issue runs (drift/error/down/degraded) per host without background schedulers.
	_, _ = d.Exec(`DELETE FROM inspection_runs WHERE host_id = ? AND status = 'ok' AND id NOT IN (SELECT id FROM inspection_runs WHERE host_id = ? AND status = 'ok' ORDER BY id DESC LIMIT 100)`, run.HostID, run.HostID)
	_, _ = d.Exec(`DELETE FROM inspection_runs WHERE host_id = ? AND status != 'ok' AND id NOT IN (SELECT id FROM inspection_runs WHERE host_id = ? AND status != 'ok' ORDER BY id DESC LIMIT 100)`, run.HostID, run.HostID)
	return nil
}

func (d *DB) ListInspectionRuns(hostID int64, limit int, filter ...string) ([]InspectionRun, error) {
	if limit <= 0 {
		limit = 50
	}
	var query string
	var args []interface{}
	if len(filter) > 0 && filter[0] == "issues" {
		query = `SELECT id, host_id, started_at, duration_ms, status, summary, details, created_at FROM inspection_runs WHERE host_id = ? AND status != 'ok' ORDER BY id DESC LIMIT ?`
		args = []interface{}{hostID, limit}
	} else {
		query = `SELECT id, host_id, started_at, duration_ms, status, summary, details, created_at FROM inspection_runs WHERE host_id = ? ORDER BY id DESC LIMIT ?`
		args = []interface{}{hostID, limit}
	}
	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []InspectionRun
	for rows.Next() {
		var r InspectionRun
		if err := rows.Scan(&r.ID, &r.HostID, &r.StartedAt, &r.DurationMs, &r.Status, &r.Summary, &r.Details, &r.CreatedAt); err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, nil
}

// Desired State operations
func (d *DB) ListDesiredRules(hostID int64) ([]DesiredRule, error) {
	rows, err := d.Query(`SELECT id, host_id, kind, target, expected, enabled FROM desired_rules WHERE host_id = ? ORDER BY kind, target`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []DesiredRule
	for rows.Next() {
		var r DesiredRule
		var enabled int
		if err := rows.Scan(&r.ID, &r.HostID, &r.Kind, &r.Target, &r.Expected, &enabled); err != nil {
			return nil, err
		}
		r.Enabled = enabled == 1
		rules = append(rules, r)
	}
	return rules, nil
}

func (d *DB) SaveDesiredRule(r *DesiredRule) error {
	enabled := 0
	if r.Enabled {
		enabled = 1
	}
	_, err := d.Exec(`INSERT INTO desired_rules(host_id, kind, target, expected, enabled) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(host_id, kind, target) DO UPDATE SET expected=excluded.expected, enabled=excluded.enabled`,
		r.HostID, r.Kind, r.Target, r.Expected, enabled)
	return err
}

func (d *DB) DeleteDesiredRule(id int64) error {
	_, err := d.Exec(`DELETE FROM desired_rules WHERE id = ?`, id)
	return err
}

// Actual items operations
func (d *DB) ListActualItems(hostID int64) ([]ActualItem, error) {
	rows, err := d.Query(`SELECT id, host_id, kind, target, current, status, details, updated_at FROM actual_items WHERE host_id = ? ORDER BY kind, target`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []ActualItem
	for rows.Next() {
		var it ActualItem
		if err := rows.Scan(&it.ID, &it.HostID, &it.Kind, &it.Target, &it.Current, &it.Status, &it.Details, &it.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, nil
}

func (d *DB) UpsertActualItem(it *ActualItem) error {
	_, err := d.Exec(`INSERT INTO actual_items(host_id, kind, target, current, status, details, updated_at) VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(host_id, kind, target) DO UPDATE SET current=excluded.current, status=excluded.status, details=excluded.details, updated_at=CURRENT_TIMESTAMP`,
		it.HostID, it.Kind, it.Target, it.Current, it.Status, it.Details)
	return err
}

// Incident operations
func (d *DB) RecordIncident(inc *Incident) error {
	_, err := d.Exec(`INSERT INTO incidents(host_id, kind, target, event_type, summary, root_cause_excerpt) VALUES (?, ?, ?, ?, ?, ?)`,
		inc.HostID, inc.Kind, inc.Target, inc.EventType, inc.Summary, inc.RootCauseExcerpt)
	return err
}

func (d *DB) ListIncidents(hostID int64, limit int) ([]Incident, error) {
	query := `SELECT id, host_id, kind, target, event_type, summary, root_cause_excerpt, created_at FROM incidents WHERE host_id = ? ORDER BY id DESC LIMIT ?`
	rows, err := d.Query(query, hostID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var incs []Incident
	for rows.Next() {
		var inc Incident
		if err := rows.Scan(&inc.ID, &inc.HostID, &inc.Kind, &inc.Target, &inc.EventType, &inc.Summary, &inc.RootCauseExcerpt, &inc.CreatedAt); err != nil {
			return nil, err
		}
		incs = append(incs, inc)
	}
	return incs, nil
}

// Alert operations
func (d *DB) CreateAlert(alt *Alert) (int64, error) {
	res, err := d.Exec(`INSERT INTO alerts(host_id, level, message, root_cause) VALUES (?, ?, ?, ?)`,
		alt.HostID, alt.Level, alt.Message, alt.RootCause)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) ListActiveAlerts() ([]Alert, error) {
	rows, err := d.Query(`SELECT a.id, COALESCE(a.host_id,0), COALESCE(h.name,'System'), a.level, a.message, a.root_cause, a.acknowledged, a.created_at
		FROM alerts a LEFT JOIN hosts h ON a.host_id = h.id WHERE a.acknowledged = 0 ORDER BY a.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Alert
	for rows.Next() {
		var a Alert
		var ack int
		if err := rows.Scan(&a.ID, &a.HostID, &a.HostName, &a.Level, &a.Message, &a.RootCause, &ack, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.Acknowledged = ack == 1
		list = append(list, a)
	}
	return list, nil
}

func (d *DB) AcknowledgeAlert(id int64) error {
	_, err := d.Exec(`UPDATE alerts SET acknowledged = 1 WHERE id = ?`, id)
	return err
}

func (d *DB) AcknowledgeAllAlerts() error {
	_, err := d.Exec(`UPDATE alerts SET acknowledged = 1 WHERE acknowledged = 0`)
	return err
}

func (d *DB) AcknowledgeHostAlerts(hostID int64) error {
	_, err := d.Exec(`UPDATE alerts SET acknowledged = 1 WHERE host_id = ? AND acknowledged = 0`, hostID)
	return err
}

// Terminal Preset operations
func (d *DB) ListTerminalPresets(hostID *int64) ([]TerminalPreset, error) {
	var query string
	var args []interface{}
	if hostID == nil {
		query = `SELECT id, host_id, name, command, sort_order, use_tmux, created_at FROM terminal_presets WHERE host_id IS NULL ORDER BY sort_order ASC, id ASC`
	} else {
		query = `SELECT id, host_id, name, command, sort_order, use_tmux, created_at FROM terminal_presets WHERE host_id IS NULL OR host_id = ? ORDER BY sort_order ASC, id ASC`
		args = append(args, *hostID)
	}

	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var presets []TerminalPreset
	for rows.Next() {
		var p TerminalPreset
		var rawHostID sql.NullInt64
		var useTmux int
		if err := rows.Scan(&p.ID, &rawHostID, &p.Name, &p.Command, &p.SortOrder, &useTmux, &p.CreatedAt); err != nil {
			return nil, err
		}
		if rawHostID.Valid {
			hid := rawHostID.Int64
			p.HostID = &hid
		}
		p.UseTmux = useTmux == 1
		presets = append(presets, p)
	}
	return presets, nil
}

func (d *DB) ListAllTerminalPresets() ([]TerminalPreset, error) {
	rows, err := d.Query(`SELECT id, host_id, name, command, sort_order, use_tmux, created_at FROM terminal_presets ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var presets []TerminalPreset
	for rows.Next() {
		var p TerminalPreset
		var rawHostID sql.NullInt64
		var useTmux int
		if err := rows.Scan(&p.ID, &rawHostID, &p.Name, &p.Command, &p.SortOrder, &useTmux, &p.CreatedAt); err != nil {
			return nil, err
		}
		if rawHostID.Valid {
			hid := rawHostID.Int64
			p.HostID = &hid
		}
		p.UseTmux = useTmux == 1
		presets = append(presets, p)
	}
	return presets, nil
}

func (d *DB) GetTerminalPreset(id int64) (*TerminalPreset, error) {
	row := d.QueryRow(`SELECT id, host_id, name, command, sort_order, use_tmux, created_at FROM terminal_presets WHERE id = ?`, id)
	var p TerminalPreset
	var rawHostID sql.NullInt64
	var useTmux int
	if err := row.Scan(&p.ID, &rawHostID, &p.Name, &p.Command, &p.SortOrder, &useTmux, &p.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if rawHostID.Valid {
		hid := rawHostID.Int64
		p.HostID = &hid
	}
	p.UseTmux = useTmux == 1
	return &p, nil
}

func (d *DB) CreateTerminalPreset(p *TerminalPreset) (int64, error) {
	useTmuxInt := 0
	if p.UseTmux {
		useTmuxInt = 1
	}
	var rawHostID interface{} = nil
	if p.HostID != nil {
		rawHostID = *p.HostID
	}
	res, err := d.Exec(`INSERT INTO terminal_presets (host_id, name, command, sort_order, use_tmux) VALUES (?, ?, ?, ?, ?)`,
		rawHostID, p.Name, p.Command, p.SortOrder, useTmuxInt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) UpdateTerminalPreset(p *TerminalPreset) error {
	useTmuxInt := 0
	if p.UseTmux {
		useTmuxInt = 1
	}
	var rawHostID interface{} = nil
	if p.HostID != nil {
		rawHostID = *p.HostID
	}
	_, err := d.Exec(`UPDATE terminal_presets SET host_id = ?, name = ?, command = ?, sort_order = ?, use_tmux = ? WHERE id = ?`,
		rawHostID, p.Name, p.Command, p.SortOrder, useTmuxInt, p.ID)
	return err
}

func (d *DB) DeleteTerminalPreset(id int64) error {
	_, err := d.Exec(`DELETE FROM terminal_presets WHERE id = ?`, id)
	return err
}

// SnapshotPayload holds the sanitized system configuration for backup and restore.
type SnapshotPayload struct {
	Version         int               `json:"version"`
	CreatedAt       time.Time         `json:"created_at"`
	Settings        map[string]string `json:"settings"`
	Hosts           []Host            `json:"hosts"`
	DesiredRules    []DesiredRule     `json:"desired_rules"`
	TerminalPresets []TerminalPreset  `json:"terminal_presets,omitempty"`
}

func (d *DB) IsFresh() (bool, error) {
	var count int
	if err := d.QueryRow("SELECT COUNT(*) FROM hosts").Scan(&count); err != nil {
		return false, err
	}
	if count > 0 {
		return false, nil
	}
	var completed string
	err := d.QueryRow("SELECT value FROM settings WHERE key = 'bootstrap_completed'").Scan(&completed)
	if err == sql.ErrNoRows {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return completed != "1", nil
}

func (d *DB) MarkBootstrapCompleted() error {
	return d.SetSetting("bootstrap_completed", "1")
}

func (d *DB) ExportSnapshot() (*SnapshotPayload, error) {
	hosts, err := d.ListHosts()
	if err != nil {
		return nil, fmt.Errorf("list hosts: %w", err)
	}

	var allRules []DesiredRule
	for _, h := range hosts {
		rules, err := d.ListDesiredRules(h.ID)
		if err != nil {
			return nil, fmt.Errorf("list rules for host %d: %w", h.ID, err)
		}
		allRules = append(allRules, rules...)
	}

	settings := make(map[string]string)
	rows, err := d.Query("SELECT key, value FROM settings")
	if err != nil {
		return nil, fmt.Errorf("select settings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		settings[k] = v
	}

	var allPresets []TerminalPreset
	pRows, err := d.Query("SELECT id, host_id, name, command, sort_order, use_tmux, created_at FROM terminal_presets ORDER BY sort_order ASC, id ASC")
	if err == nil {
		defer pRows.Close()
		for pRows.Next() {
			var p TerminalPreset
			var rawHostID sql.NullInt64
			var useTmux int
			if err := pRows.Scan(&p.ID, &rawHostID, &p.Name, &p.Command, &p.SortOrder, &useTmux, &p.CreatedAt); err == nil {
				if rawHostID.Valid {
					hid := rawHostID.Int64
					p.HostID = &hid
				}
				p.UseTmux = useTmux == 1
				allPresets = append(allPresets, p)
			}
		}
	}

	return &SnapshotPayload{
		Version:         1,
		CreatedAt:       time.Now().UTC(),
		Settings:        settings,
		Hosts:           hosts,
		DesiredRules:    allRules,
		TerminalPresets: allPresets,
	}, nil
}

func (d *DB) ImportSnapshot(payload *SnapshotPayload) error {
	if payload == nil {
		return fmt.Errorf("empty snapshot payload")
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	tables := []string{"alerts", "incidents", "actual_items", "desired_rules", "hosts", "terminal_presets"}
	for _, t := range tables {
		if _, err := tx.Exec("DELETE FROM " + t); err != nil {
			return fmt.Errorf("clear %s: %w", t, err)
		}
	}

	for _, h := range payload.Hosts {
		onlineInt := 0
		if h.InternetOnline {
			onlineInt = 1
		}
		_, err := tx.Exec(`INSERT INTO hosts (
			id, name, host, port, user, custom_key, status, os_info, kernel, uptime, cpu_load,
			ram_used_bytes, ram_total_bytes, disk_used_bytes, disk_total_bytes, lifecycle_score, lifecycle_notes, lifecycle_breakdown,
			created_at, net_rx_bytes, net_tx_bytes, net_rx_speed_bps, net_tx_speed_bps,
			internet_online, internet_latency_ms, public_ip, active_conn_count, failed_logins_count,
			top_connections, listening_ports, notes, sort_order, group_name,
			commission_date, bios_date, os_install_epoch
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			h.ID, h.Name, h.Host, h.Port, h.User, h.CustomKey, h.Status, h.OSInfo, h.Kernel, h.Uptime, h.CPULoad,
			h.RAMUsedBytes, h.RAMTotalBytes, h.DiskUsedBytes, h.DiskTotalBytes, h.LifecycleScore, h.LifecycleNotes, h.LifecycleBreakdown,
			h.CreatedAt, h.NetRxBytes, h.NetTxBytes, h.NetRxSpeedBps, h.NetTxSpeedBps,
			onlineInt, h.InternetLatencyMs, h.PublicIP, h.ActiveConnCount, h.FailedLoginsCount,
			h.TopConnections, h.ListeningPorts, h.Notes, h.SortOrder, h.GroupName,
			h.CommissionDate, h.BIOSDate, h.OSInstallEpoch,
		)
		if err != nil {
			return fmt.Errorf("insert host %d: %w", h.ID, err)
		}
	}

	for _, r := range payload.DesiredRules {
		enabledInt := 0
		if r.Enabled {
			enabledInt = 1
		}
		_, err := tx.Exec(`INSERT INTO desired_rules (id, host_id, kind, target, expected, enabled) VALUES (?, ?, ?, ?, ?, ?)`,
			r.ID, r.HostID, r.Kind, r.Target, r.Expected, enabledInt)
		if err != nil {
			return fmt.Errorf("insert desired rule %d: %w", r.ID, err)
		}
	}

	for _, p := range payload.TerminalPresets {
		useTmuxInt := 0
		if p.UseTmux {
			useTmuxInt = 1
		}
		var hid interface{} = nil
		if p.HostID != nil {
			hid = *p.HostID
		}
		_, _ = tx.Exec(`INSERT INTO terminal_presets (id, host_id, name, command, sort_order, use_tmux) VALUES (?, ?, ?, ?, ?, ?)`,
			p.ID, hid, p.Name, p.Command, p.SortOrder, useTmuxInt)
	}

	for k, v := range payload.Settings {
		_, err := tx.Exec(`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, v)
		if err != nil {
			return fmt.Errorf("insert setting %s: %w", k, err)
		}
	}

	_, _ = tx.Exec(`INSERT INTO settings (key, value) VALUES ('bootstrap_completed', '1') ON CONFLICT(key) DO UPDATE SET value = '1'`)

	return tx.Commit()
}
