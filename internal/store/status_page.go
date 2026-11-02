package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

type StatusPageTarget struct {
	ID            int64     `json:"id"`
	TargetType    string    `json:"target_type"` // "host" or "desired_rule"
	HostID        int64     `json:"host_id"`
	DesiredRuleID *int64    `json:"desired_rule_id,omitempty"`
	PublicName    string    `json:"public_name"`
	GroupName     string    `json:"group_name"`
	SortOrder     int       `json:"sort_order"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`

	// Enriched fields for Admin UI
	HostName   string `json:"host_name,omitempty"`
	RuleKind   string `json:"rule_kind,omitempty"`
	RuleTarget string `json:"rule_target,omitempty"`
}

type StatusPageDailyRollup struct {
	ID               int64     `json:"id"`
	TargetID         int64     `json:"target_id"`
	Date             string    `json:"date"` // YYYY-MM-DD
	TotalChecks      int       `json:"total_checks"`
	SuccessfulChecks int       `json:"successful_checks"`
	DegradedChecks   int       `json:"degraded_checks"`
	OutageChecks     int       `json:"outage_checks"`
	UptimePercentage float64   `json:"uptime_percentage"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type StatusPageIncident struct {
	ID              int64                      `json:"id"`
	Title           string                     `json:"title"`
	Status          string                     `json:"status"` // "investigating", "identified", "monitoring", "resolved"
	Impact          string                     `json:"impact"` // "none", "minor", "major", "critical"
	Message         string                     `json:"message"`
	AffectedTargets []int64                    `json:"affected_targets"`
	CreatedAt       time.Time                  `json:"created_at"`
	UpdatedAt       time.Time                  `json:"updated_at"`
	ResolvedAt      *time.Time                 `json:"resolved_at,omitempty"`
	Updates         []StatusPageIncidentUpdate `json:"updates,omitempty"`
}

type StatusPageIncidentUpdate struct {
	ID         int64     `json:"id"`
	IncidentID int64     `json:"incident_id"`
	Status     string    `json:"status"`
	Message    string    `json:"message"`
	CreatedAt  time.Time `json:"created_at"`
}

// Public DTOs (Data Masking)
type PublicStatusSummary struct {
	Page            PublicPageConfig      `json:"page"`
	Status          PublicSystemStatus    `json:"status"`
	Targets         []PublicTargetSummary `json:"targets"`
	ActiveIncidents []StatusPageIncident  `json:"active_incidents"`
	UpdatedAt       time.Time             `json:"updated_at"`
}

type PublicPageConfig struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	SupportURL  string `json:"support_url"`
	LogoURL     string `json:"logo_url"`
}

type PublicSystemStatus struct {
	Indicator   string `json:"indicator"` // "operational", "degraded", "major_outage", "maintenance"
	Description string `json:"description"`
}

type PublicTargetSummary struct {
	ID           int64                    `json:"id"`
	Name         string                   `json:"name"`
	Group        string                   `json:"group"`
	Status       string                   `json:"status"` // "operational", "degraded", "outage"
	Uptime90d    float64                  `json:"uptime_90d"`
	DailyHistory []PublicDailyRollupPoint `json:"daily_history"`
}

type PublicDailyRollupPoint struct {
	Date   string  `json:"date"`
	Status string  `json:"status"`
	Uptime float64 `json:"uptime"`
}

// CreateStatusPageTarget inserts a new status page target.
func (d *DB) CreateStatusPageTarget(t *StatusPageTarget) error {
	var ruleID sql.NullInt64
	if t.DesiredRuleID != nil && *t.DesiredRuleID > 0 {
		ruleID = sql.NullInt64{Int64: *t.DesiredRuleID, Valid: true}
	}
	enabledInt := 0
	if t.Enabled {
		enabledInt = 1
	}

	query := `INSERT INTO status_page_targets (target_type, host_id, desired_rule_id, public_name, group_name, sort_order, enabled)
	VALUES (?, ?, ?, ?, ?, ?, ?)`
	res, err := d.Exec(query, t.TargetType, t.HostID, ruleID, t.PublicName, t.GroupName, t.SortOrder, enabledInt)
	if err != nil {
		return err
	}
	t.ID, err = res.LastInsertId()
	return err
}

// GetStatusPageTargets returns all targets enriched with host and rule names for Admin UI.
func (d *DB) GetStatusPageTargets() ([]StatusPageTarget, error) {
	query := `SELECT t.id, t.target_type, t.host_id, t.desired_rule_id, t.public_name, t.group_name, t.sort_order, t.enabled, t.created_at,
		COALESCE(h.name, '') as host_name,
		COALESCE(r.kind, '') as rule_kind,
		COALESCE(r.target, '') as rule_target
	FROM status_page_targets t
	LEFT JOIN hosts h ON t.host_id = h.id
	LEFT JOIN desired_rules r ON t.desired_rule_id = r.id
	ORDER BY t.group_name ASC, t.sort_order ASC, t.id ASC`

	rows, err := d.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var targets []StatusPageTarget
	for rows.Next() {
		var t StatusPageTarget
		var ruleID sql.NullInt64
		var enabledInt int
		err := rows.Scan(
			&t.ID, &t.TargetType, &t.HostID, &ruleID, &t.PublicName, &t.GroupName, &t.SortOrder, &enabledInt, &t.CreatedAt,
			&t.HostName, &t.RuleKind, &t.RuleTarget,
		)
		if err != nil {
			return nil, err
		}
		if ruleID.Valid {
			val := ruleID.Int64
			t.DesiredRuleID = &val
		}
		t.Enabled = enabledInt == 1
		targets = append(targets, t)
	}
	return targets, rows.Err()
}

// GetStatusPageTargetByID retrieves a target by ID.
func (d *DB) GetStatusPageTargetByID(id int64) (*StatusPageTarget, error) {
	query := `SELECT t.id, t.target_type, t.host_id, t.desired_rule_id, t.public_name, t.group_name, t.sort_order, t.enabled, t.created_at,
		COALESCE(h.name, '') as host_name,
		COALESCE(r.kind, '') as rule_kind,
		COALESCE(r.target, '') as rule_target
	FROM status_page_targets t
	LEFT JOIN hosts h ON t.host_id = h.id
	LEFT JOIN desired_rules r ON t.desired_rule_id = r.id
	WHERE t.id = ?`

	var t StatusPageTarget
	var ruleID sql.NullInt64
	var enabledInt int
	err := d.QueryRow(query, id).Scan(
		&t.ID, &t.TargetType, &t.HostID, &ruleID, &t.PublicName, &t.GroupName, &t.SortOrder, &enabledInt, &t.CreatedAt,
		&t.HostName, &t.RuleKind, &t.RuleTarget,
	)
	if err != nil {
		return nil, err
	}
	if ruleID.Valid {
		val := ruleID.Int64
		t.DesiredRuleID = &val
	}
	t.Enabled = enabledInt == 1
	return &t, nil
}

// UpdateStatusPageTarget updates an existing target.
func (d *DB) UpdateStatusPageTarget(t *StatusPageTarget) error {
	var ruleID sql.NullInt64
	if t.DesiredRuleID != nil && *t.DesiredRuleID > 0 {
		ruleID = sql.NullInt64{Int64: *t.DesiredRuleID, Valid: true}
	}
	enabledInt := 0
	if t.Enabled {
		enabledInt = 1
	}

	query := `UPDATE status_page_targets
	SET target_type = ?, host_id = ?, desired_rule_id = ?, public_name = ?, group_name = ?, sort_order = ?, enabled = ?
	WHERE id = ?`
	_, err := d.Exec(query, t.TargetType, t.HostID, ruleID, t.PublicName, t.GroupName, t.SortOrder, enabledInt, t.ID)
	return err
}

// DeleteStatusPageTarget removes a target and cascades to rollups.
func (d *DB) DeleteStatusPageTarget(id int64) error {
	_, err := d.Exec(`DELETE FROM status_page_targets WHERE id = ?`, id)
	return err
}

// GetActiveStatusPageTargetsForHost returns enabled targets associated with a given host.
func (d *DB) GetActiveStatusPageTargetsForHost(hostID int64) ([]StatusPageTarget, error) {
	query := `SELECT id, target_type, host_id, desired_rule_id, public_name, group_name, sort_order, enabled, created_at
	FROM status_page_targets
	WHERE host_id = ? AND enabled = 1`

	rows, err := d.Query(query, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var targets []StatusPageTarget
	for rows.Next() {
		var t StatusPageTarget
		var ruleID sql.NullInt64
		var enabledInt int
		if err := rows.Scan(&t.ID, &t.TargetType, &t.HostID, &ruleID, &t.PublicName, &t.GroupName, &t.SortOrder, &enabledInt, &t.CreatedAt); err != nil {
			return nil, err
		}
		if ruleID.Valid {
			val := ruleID.Int64
			t.DesiredRuleID = &val
		}
		t.Enabled = enabledInt == 1
		targets = append(targets, t)
	}
	return targets, rows.Err()
}

// RecordDailyRollup updates or inserts the daily rollup stats for targetID on date using an atomic UPSERT.
func (d *DB) RecordDailyRollup(targetID int64, date string, status string) error {
	succInc := 0
	degInc := 0
	outInc := 0
	initialUptime := 100.0

	switch status {
	case "operational":
		succInc = 1
		initialUptime = 100.0
	case "degraded":
		degInc = 1
		initialUptime = 50.0
	case "outage":
		outInc = 1
		initialUptime = 0.0
	default:
		succInc = 1
		initialUptime = 100.0
	}

	query := `INSERT INTO status_page_daily_rollups (
		target_id, date, total_checks, successful_checks, degraded_checks, outage_checks, uptime_percentage, updated_at
	) VALUES (
		?, ?, 1, ?, ?, ?, ?, CURRENT_TIMESTAMP
	) ON CONFLICT(target_id, date) DO UPDATE SET
		total_checks = total_checks + 1,
		successful_checks = successful_checks + excluded.successful_checks,
		degraded_checks = degraded_checks + excluded.degraded_checks,
		outage_checks = outage_checks + excluded.outage_checks,
		uptime_percentage = ROUND(((successful_checks + excluded.successful_checks + 0.5 * (degraded_checks + excluded.degraded_checks)) * 100.0) / (total_checks + 1), 2),
		updated_at = CURRENT_TIMESTAMP`

	_, err := d.Exec(query, targetID, date, succInc, degInc, outInc, initialUptime)
	return err
}

// GetDailyRollups retrieves the last N days of rollups for targetID in descending order.
func (d *DB) GetDailyRollups(targetID int64, days int) ([]StatusPageDailyRollup, error) {
	query := `SELECT id, target_id, date, total_checks, successful_checks, degraded_checks, outage_checks, uptime_percentage, created_at, updated_at
	FROM status_page_daily_rollups
	WHERE target_id = ? AND date >= date('now', ?)
	ORDER BY date ASC`

	daysParam := fmt.Sprintf("-%d days", days)
	rows, err := d.Query(query, targetID, daysParam)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []StatusPageDailyRollup
	for rows.Next() {
		var r StatusPageDailyRollup
		err := rows.Scan(
			&r.ID, &r.TargetID, &r.Date, &r.TotalChecks, &r.SuccessfulChecks, &r.DegradedChecks, &r.OutageChecks,
			&r.UptimePercentage, &r.CreatedAt, &r.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	return list, rows.Err()
}

// PruneDailyRollups deletes rollup rows older than days.
func (d *DB) PruneDailyRollups(days int) (int64, error) {
	daysParam := fmt.Sprintf("-%d days", days)
	res, err := d.Exec(`DELETE FROM status_page_daily_rollups WHERE date < date('now', ?)`, daysParam)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CreateStatusPageIncident inserts a new public incident.
func (d *DB) CreateStatusPageIncident(inc *StatusPageIncident) (int64, error) {
	targetsJSON, err := json.Marshal(inc.AffectedTargets)
	if err != nil {
		targetsJSON = []byte("[]")
	}

	var resolvedAt *time.Time
	if inc.Status == "resolved" {
		now := time.Now().UTC()
		resolvedAt = &now
	}

	query := `INSERT INTO status_page_incidents (title, status, impact, message, affected_targets, resolved_at)
	VALUES (?, ?, ?, ?, ?, ?)`
	res, err := d.Exec(query, inc.Title, inc.Status, inc.Impact, inc.Message, string(targetsJSON), resolvedAt)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	inc.ID = id

	// Automatically record initial update entry
	if strings.TrimSpace(inc.Message) != "" {
		_ = d.AddStatusPageIncidentUpdate(id, inc.Status, inc.Message)
	}
	return id, nil
}

// GetStatusPageIncidents retrieves incidents. If activeOnly is true, only non-resolved are returned.
func (d *DB) GetStatusPageIncidents(activeOnly bool) ([]StatusPageIncident, error) {
	query := `SELECT id, title, status, impact, message, affected_targets, created_at, updated_at, resolved_at
	FROM status_page_incidents`
	if activeOnly {
		query += ` WHERE status != 'resolved'`
	}
	query += ` ORDER BY created_at DESC LIMIT 50`

	rows, err := d.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []StatusPageIncident
	for rows.Next() {
		var inc StatusPageIncident
		var targetsStr string
		var resolvedAt sql.NullTime

		err := rows.Scan(&inc.ID, &inc.Title, &inc.Status, &inc.Impact, &inc.Message, &targetsStr, &inc.CreatedAt, &inc.UpdatedAt, &resolvedAt)
		if err != nil {
			return nil, err
		}
		if resolvedAt.Valid {
			inc.ResolvedAt = &resolvedAt.Time
		}
		if targetsStr != "" {
			_ = json.Unmarshal([]byte(targetsStr), &inc.AffectedTargets)
		}
		if inc.AffectedTargets == nil {
			inc.AffectedTargets = []int64{}
		}

		// Load updates
		updates, err := d.GetStatusPageIncidentUpdates(inc.ID)
		if err == nil {
			inc.Updates = updates
		}
		list = append(list, inc)
	}
	return list, rows.Err()
}

// GetStatusPageIncidentByID retrieves an incident by ID.
func (d *DB) GetStatusPageIncidentByID(id int64) (*StatusPageIncident, error) {
	query := `SELECT id, title, status, impact, message, affected_targets, created_at, updated_at, resolved_at
	FROM status_page_incidents WHERE id = ?`

	var inc StatusPageIncident
	var targetsStr string
	var resolvedAt sql.NullTime

	err := d.QueryRow(query, id).Scan(&inc.ID, &inc.Title, &inc.Status, &inc.Impact, &inc.Message, &targetsStr, &inc.CreatedAt, &inc.UpdatedAt, &resolvedAt)
	if err != nil {
		return nil, err
	}
	if resolvedAt.Valid {
		inc.ResolvedAt = &resolvedAt.Time
	}
	if targetsStr != "" {
		_ = json.Unmarshal([]byte(targetsStr), &inc.AffectedTargets)
	}
	if inc.AffectedTargets == nil {
		inc.AffectedTargets = []int64{}
	}

	updates, _ := d.GetStatusPageIncidentUpdates(inc.ID)
	inc.Updates = updates
	return &inc, nil
}

// UpdateStatusPageIncident updates an existing incident.
func (d *DB) UpdateStatusPageIncident(inc *StatusPageIncident) error {
	targetsJSON, err := json.Marshal(inc.AffectedTargets)
	if err != nil {
		targetsJSON = []byte("[]")
	}

	var resolvedAt *time.Time
	if inc.Status == "resolved" {
		now := time.Now().UTC()
		resolvedAt = &now
	}

	query := `UPDATE status_page_incidents
	SET title = ?, status = ?, impact = ?, message = ?, affected_targets = ?, updated_at = CURRENT_TIMESTAMP, resolved_at = ?
	WHERE id = ?`
	_, err = d.Exec(query, inc.Title, inc.Status, inc.Impact, inc.Message, string(targetsJSON), resolvedAt, inc.ID)
	return err
}

// DeleteStatusPageIncident removes an incident and cascaded updates.
func (d *DB) DeleteStatusPageIncident(id int64) error {
	_, err := d.Exec(`DELETE FROM status_page_incidents WHERE id = ?`, id)
	return err
}

// AddStatusPageIncidentUpdate appends a progress entry and updates incident status.
func (d *DB) AddStatusPageIncidentUpdate(incidentID int64, status, message string) error {
	query := `INSERT INTO status_page_incident_updates (incident_id, status, message) VALUES (?, ?, ?)`
	_, err := d.Exec(query, incidentID, status, message)
	if err != nil {
		return err
	}

	var resolvedAt *time.Time
	if status == "resolved" {
		now := time.Now().UTC()
		resolvedAt = &now
	}

	updateParent := `UPDATE status_page_incidents
	SET status = ?, message = ?, updated_at = CURRENT_TIMESTAMP, resolved_at = COALESCE(?, resolved_at)
	WHERE id = ?`
	_, err = d.Exec(updateParent, status, message, resolvedAt, incidentID)
	return err
}

// GetStatusPageIncidentUpdates retrieves all updates for an incident in chronological order.
func (d *DB) GetStatusPageIncidentUpdates(incidentID int64) ([]StatusPageIncidentUpdate, error) {
	query := `SELECT id, incident_id, status, message, created_at
	FROM status_page_incident_updates
	WHERE incident_id = ?
	ORDER BY created_at ASC`

	rows, err := d.Query(query, incidentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []StatusPageIncidentUpdate
	for rows.Next() {
		var u StatusPageIncidentUpdate
		if err := rows.Scan(&u.ID, &u.IncidentID, &u.Status, &u.Message, &u.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, u)
	}
	return list, rows.Err()
}

// GetPublicStatusSummary compiles the complete sanitized public status summary for /api/status-page/summary.
func (d *DB) GetPublicStatusSummary() (*PublicStatusSummary, error) {
	title, _ := d.GetSetting("status_page_title")
	if strings.TrimSpace(title) == "" {
		title = "Pantau System Status"
	}
	desc, _ := d.GetSetting("status_page_header_text")
	if strings.TrimSpace(desc) == "" {
		desc = "Real-time service status and incident updates"
	}
	logo, _ := d.GetSetting("status_page_logo_url")
	supportURL, _ := d.GetSetting("status_page_support_url")

	summary := &PublicStatusSummary{
		Page: PublicPageConfig{
			Title:       title,
			Description: desc,
			LogoURL:     logo,
			SupportURL:  supportURL,
		},
		Status: PublicSystemStatus{
			Indicator:   "operational",
			Description: "All Systems Operational",
		},
		Targets:         []PublicTargetSummary{},
		ActiveIncidents: []StatusPageIncident{},
		UpdatedAt:       time.Now().UTC(),
	}

	// Fetch active targets
	targetsQuery := `SELECT id, target_type, host_id, desired_rule_id, public_name, group_name
	FROM status_page_targets
	WHERE enabled = 1
	ORDER BY group_name ASC, sort_order ASC, id ASC`

	tRows, err := d.Query(targetsQuery)
	if err != nil {
		return nil, err
	}
	defer tRows.Close()

	type targetItem struct {
		id            int64
		targetType    string
		hostID        int64
		desiredRuleID sql.NullInt64
		publicName    string
		groupName     string
	}
	var targetItems []targetItem
	for tRows.Next() {
		var item targetItem
		if err := tRows.Scan(&item.id, &item.targetType, &item.hostID, &item.desiredRuleID, &item.publicName, &item.groupName); err == nil {
			targetItems = append(targetItems, item)
		}
	}
	tRows.Close()

	nowDate := time.Now().UTC()
	anyOutage := false
	anyDegraded := false

	for _, item := range targetItems {
		// Determine live current status of this target
		currentStatus := "operational"
		if item.targetType == "host" {
			var hStatus string
			err := d.QueryRow(`SELECT status FROM hosts WHERE id = ?`, item.hostID).Scan(&hStatus)
			if err == nil {
				if hStatus == "down" {
					currentStatus = "outage"
				} else if hStatus == "degraded" || hStatus == "unknown" {
					currentStatus = "degraded"
				}
			}
		} else if item.targetType == "desired_rule" && item.desiredRuleID.Valid {
			var hStatus string
			var ruleKind, ruleTarget, expected string
			_ = d.QueryRow(`SELECT status FROM hosts WHERE id = ?`, item.hostID).Scan(&hStatus)
			_ = d.QueryRow(`SELECT kind, target, expected FROM desired_rules WHERE id = ?`, item.desiredRuleID.Int64).Scan(&ruleKind, &ruleTarget, &expected)

			if hStatus == "down" {
				currentStatus = "outage"
			} else {
				var actualStatus string
				err := d.QueryRow(`SELECT status FROM actual_items WHERE host_id = ? AND kind = ? AND target = ?`, item.hostID, ruleKind, ruleTarget).Scan(&actualStatus)
				if err == nil && actualStatus != "" && !strings.EqualFold(actualStatus, expected) && actualStatus != "running" && actualStatus != "active" {
					if ruleKind == "service" || ruleKind == "port" || ruleKind == "container" || ruleKind == "process" {
						currentStatus = "outage"
					} else {
						currentStatus = "degraded"
					}
				}
			}
		}

		if currentStatus == "outage" {
			anyOutage = true
		} else if currentStatus == "degraded" {
			anyDegraded = true
		}

		// Fetch last 90 days rollups
		rollups, _ := d.GetDailyRollups(item.id, 90)
		rollupMap := make(map[string]StatusPageDailyRollup)
		totalAllChecks := 0
		succAllChecks := 0
		degAllChecks := 0

		for _, r := range rollups {
			rollupMap[r.Date] = r
			totalAllChecks += r.TotalChecks
			succAllChecks += r.SuccessfulChecks
			degAllChecks += r.DegradedChecks
		}

		uptime90d := 100.0
		if totalAllChecks > 0 {
			uptime90d = math.Round(((float64(succAllChecks)+0.5*float64(degAllChecks))*100.0/float64(totalAllChecks))*100) / 100
		}

		// Fill 90-day points
		dailyHistory := make([]PublicDailyRollupPoint, 90)
		for i := 89; i >= 0; i-- {
			d := nowDate.AddDate(0, 0, -(89 - i)).Format("2006-01-02")
			pointStatus := "operational"
			pointUptime := 100.0

			if r, found := rollupMap[d]; found {
				pointUptime = r.UptimePercentage
				if r.UptimePercentage < 95.0 || (r.OutageChecks > 0 && r.TotalChecks <= 2) {
					pointStatus = "outage"
				} else if r.UptimePercentage < 100.0 || r.DegradedChecks > 0 || r.OutageChecks > 0 {
					pointStatus = "degraded"
				}
			} else if (89 - i) == 0 {
				// Today default matches current status
				pointStatus = currentStatus
				if currentStatus == "outage" {
					pointUptime = 0.0
				} else if currentStatus == "degraded" {
					pointUptime = 50.0
				} else {
					pointUptime = 100.0
				}
			} else {
				pointStatus = "operational"
				pointUptime = 100.0
			}

			dailyHistory[i] = PublicDailyRollupPoint{
				Date:   d,
				Status: pointStatus,
				Uptime: pointUptime,
			}
		}

		summary.Targets = append(summary.Targets, PublicTargetSummary{
			ID:           item.id,
			Name:         item.publicName,
			Group:        item.groupName,
			Status:       currentStatus,
			Uptime90d:    uptime90d,
			DailyHistory: dailyHistory,
		})
	}

	// Fetch active incidents
	incidents, _ := d.GetStatusPageIncidents(true)
	summary.ActiveIncidents = incidents

	// Determine overall system indicator
	hasCriticalIncident := false
	hasMinorIncident := false
	hasMaintenanceIncident := false

	for _, inc := range incidents {
		switch inc.Impact {
		case "critical", "major":
			hasCriticalIncident = true
		case "minor":
			hasMinorIncident = true
		case "none":
			hasMaintenanceIncident = true
		}
	}

	if anyOutage || hasCriticalIncident {
		summary.Status.Indicator = "major_outage"
		summary.Status.Description = "Major System Outage Detected"
	} else if anyDegraded || hasMinorIncident {
		summary.Status.Indicator = "degraded"
		summary.Status.Description = "Degraded Performance on Some Services"
	} else if hasMaintenanceIncident {
		summary.Status.Indicator = "maintenance"
		summary.Status.Description = "Scheduled Maintenance in Progress"
	} else {
		summary.Status.Indicator = "operational"
		summary.Status.Description = "All Systems Operational"
	}

	return summary, nil
}
