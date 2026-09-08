package store

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
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
	DiskUsedBytes  int64     `json:"disk_used_bytes"`
	DiskTotalBytes int64     `json:"disk_total_bytes"`
	LifecycleScore int       `json:"lifecycle_score"`
	LifecycleNotes string    `json:"lifecycle_notes"`
	LastInspected  *time.Time `json:"last_inspected"`
	CreatedAt      time.Time `json:"created_at"`
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
		lifecycle_score INTEGER DEFAULT 100,
		lifecycle_notes TEXT DEFAULT '',
		last_inspected DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
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
	`
	_, err := d.Exec(schema)
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

	// 2. Ensure global ED25519 SSH keypair exists
	var privKey string
	err = d.QueryRow("SELECT value FROM settings WHERE key = 'ssh_private_key'").Scan(&privKey)
	if err == sql.ErrNoRows {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return fmt.Errorf("generate ed25519 key: %w", err)
		}

		privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
		if err != nil {
			return fmt.Errorf("marshal pkcs8: %w", err)
		}

		pemBlock := pem.EncodeToMemory(&pem.Block{
			Type:  "PRIVATE KEY",
			Bytes: privBytes,
		})

		sshPub, err := ssh.NewPublicKey(pub)
		if err != nil {
			return fmt.Errorf("new ssh pubkey: %w", err)
		}
		pubAuthorized := string(ssh.MarshalAuthorizedKey(sshPub))

		tx, err := d.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		if _, err := tx.Exec("INSERT INTO settings(key, value) VALUES ('ssh_private_key', ?)", string(pemBlock)); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT INTO settings(key, value) VALUES ('ssh_public_key', ?)", pubAuthorized); err != nil {
			return err
		}
		return tx.Commit()
	}

	return nil
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

// Host operations
func (d *DB) ListHosts() ([]Host, error) {
	rows, err := d.Query(`SELECT id, name, host, port, user, COALESCE(custom_key,''), status, os_info, kernel, uptime, cpu_load, ram_used_bytes, ram_total_bytes, disk_used_bytes, disk_total_bytes, lifecycle_score, lifecycle_notes, last_inspected, created_at FROM hosts ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Host
	for rows.Next() {
		var h Host
		var inspected sql.NullTime
		if err := rows.Scan(&h.ID, &h.Name, &h.Host, &h.Port, &h.User, &h.CustomKey, &h.Status, &h.OSInfo, &h.Kernel, &h.Uptime, &h.CPULoad, &h.RAMUsedBytes, &h.RAMTotalBytes, &h.DiskUsedBytes, &h.DiskTotalBytes, &h.LifecycleScore, &h.LifecycleNotes, &inspected, &h.CreatedAt); err != nil {
			return nil, err
		}
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
	err := d.QueryRow(`SELECT id, name, host, port, user, COALESCE(custom_key,''), status, os_info, kernel, uptime, cpu_load, ram_used_bytes, ram_total_bytes, disk_used_bytes, disk_total_bytes, lifecycle_score, lifecycle_notes, last_inspected, created_at FROM hosts WHERE id = ?`, id).Scan(
		&h.ID, &h.Name, &h.Host, &h.Port, &h.User, &h.CustomKey, &h.Status, &h.OSInfo, &h.Kernel, &h.Uptime, &h.CPULoad, &h.RAMUsedBytes, &h.RAMTotalBytes, &h.DiskUsedBytes, &h.DiskTotalBytes, &h.LifecycleScore, &h.LifecycleNotes, &inspected, &h.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if inspected.Valid {
		h.LastInspected = &inspected.Time
	}
	return &h, nil
}

func (d *DB) CreateHost(h *Host) (int64, error) {
	res, err := d.Exec(`INSERT INTO hosts (name, host, port, user, custom_key, status) VALUES (?, ?, ?, ?, ?, 'unknown')`, h.Name, h.Host, h.Port, h.User, h.CustomKey)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) UpdateHost(h *Host) error {
	_, err := d.Exec(`UPDATE hosts SET name=?, host=?, port=?, user=?, custom_key=? WHERE id=?`, h.Name, h.Host, h.Port, h.User, h.CustomKey, h.ID)
	return err
}

func (d *DB) DeleteHost(id int64) error {
	_, err := d.Exec(`DELETE FROM hosts WHERE id=?`, id)
	return err
}

func (d *DB) UpdateHostInspection(h *Host) error {
	now := time.Now()
	_, err := d.Exec(`UPDATE hosts SET status=?, os_info=?, kernel=?, uptime=?, cpu_load=?, ram_used_bytes=?, ram_total_bytes=?, disk_used_bytes=?, disk_total_bytes=?, lifecycle_score=?, lifecycle_notes=?, last_inspected=? WHERE id=?`,
		h.Status, h.OSInfo, h.Kernel, h.Uptime, h.CPULoad, h.RAMUsedBytes, h.RAMTotalBytes, h.DiskUsedBytes, h.DiskTotalBytes, h.LifecycleScore, h.LifecycleNotes, now, h.ID)
	return err
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
