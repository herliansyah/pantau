package storagetriage

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/logger"
	"pantau/internal/store"
)

var (
	ErrBusy         = errors.New("a storage triage probe is already in progress for this host")
	safeUnitRegex   = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)
	journalValRegex = regexp.MustCompile(`([0-9.]+)\s*([KMGTkmgt]?[Bb]?)`)
)

type DirectoryHog struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	HumanSize string `json:"human_size"`
}

type FileHog struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	HumanSize string `json:"human_size"`
}

type RunawayFile struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	HumanSize string `json:"human_size"`
}

type GhostFile struct {
	Command   string `json:"command"`
	PID       string `json:"pid"`
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	HumanSize string `json:"human_size"`
}

type ReclaimItem struct {
	Key            string `json:"key"`
	Name           string `json:"name"`
	NameID         string `json:"name_id,omitempty"`
	Description    string `json:"description"`
	DescriptionID  string `json:"description_id,omitempty"`
	EstimatedBytes int64  `json:"estimated_bytes"`
	HumanSize      string `json:"human_size"`
	Command        string `json:"command"`
	Available      bool   `json:"available"`
}

type PartitionInfo struct {
	MountPoint  string  `json:"mount_point"`
	Filesystem  string  `json:"filesystem"`
	TotalBytes  int64   `json:"total_bytes"`
	UsedBytes   int64   `json:"used_bytes"`
	FreeBytes   int64   `json:"free_bytes"`
	UsedPercent float64 `json:"used_percent"`
}

type TriageResult struct {
	HostID            int64          `json:"host_id"`
	HostName          string         `json:"host_name"`
	ScanDurationMs    int64          `json:"scan_duration_ms"`
	ScannedAt         string         `json:"scanned_at,omitempty"`
	IsRoot            bool           `json:"is_root"`
	HasSudo           bool           `json:"has_sudo"`
	WriteRateBps      int64          `json:"write_rate_bps"`
	Partition         PartitionInfo  `json:"partition"`
	SpaceHogs         []DirectoryHog `json:"space_hogs"`
	TopFiles          []FileHog      `json:"top_files"`
	RunawayFiles      []RunawayFile  `json:"runaway_files"`
	GhostFiles        []GhostFile    `json:"ghost_files"`
	Reclaimables      []ReclaimItem  `json:"reclaimables"`
	TotalReclaimBytes int64          `json:"total_reclaim_bytes"`
	TotalReclaimHuman string         `json:"total_reclaim_human"`
	Warning           string         `json:"warning,omitempty"`
	WarningID         string         `json:"warning_id,omitempty"`
}

type ReclaimRequest struct {
	ActionKey    string `json:"action_key"`
	TargetPath   string `json:"target_path,omitempty"`
	SudoPassword string `json:"sudo_password,omitempty"`
}

type ReclaimResult struct {
	ActionKey string `json:"action_key"`
	Command   string `json:"command"`
	Success   bool   `json:"success"`
	Output    string `json:"output"`
	Error     string `json:"error,omitempty"`
}

type SystemLoadState string

const (
	LoadStateNormal   SystemLoadState = "normal"
	LoadStateBusy     SystemLoadState = "busy"
	LoadStateVeryBusy SystemLoadState = "very_busy"

	WriteSpeedBusyThresholdBps     int64 = 15 * 1024 * 1024 // 15 MB/s
	WriteSpeedVeryBusyThresholdBps int64 = 40 * 1024 * 1024 // 40 MB/s

	CooldownNormalSeconds   = 120 // 2 minutes
	CooldownBusySeconds     = 300 // 5 minutes
	CooldownVeryBusySeconds = 600 // 10 minutes
)

type ThrottleStatus struct {
	Allowed          bool            `json:"allowed"`
	State            SystemLoadState `json:"state"`
	Reason           string          `json:"reason,omitempty"`
	ReasonID         string          `json:"reason_id,omitempty"`
	CooldownSeconds  int             `json:"cooldown_seconds"`
	RemainingSeconds int             `json:"remaining_seconds"`
	CanForce         bool            `json:"can_force"`
	WriteSpeedBps    int64           `json:"write_speed_bps"`
	CPULoad1m        float64         `json:"cpu_load_1m"`
	CPUCores         int             `json:"cpu_cores"`
	LastScannedAt    *time.Time      `json:"last_scanned_at,omitempty"`
}

type RateLimitError struct {
	Status *ThrottleStatus
}

func (e *RateLimitError) Error() string {
	if e.Status != nil && e.Status.Reason != "" {
		return e.Status.Reason
	}
	return "storage triage rate limit exceeded"
}

type Service struct {
	db            *store.DB
	inspector     *inspector.Inspector
	inFlight      sync.Map // hostID -> context.CancelFunc
	lastScanTimes sync.Map // hostID -> time.Time
	lastResults   sync.Map // hostID -> *TriageResult
}

func NewService(db *store.DB, ins *inspector.Inspector) *Service {
	return &Service{
		db:        db,
		inspector: ins,
	}
}

func FormatBytes(b int64) string {
	if b <= 0 {
		return "0 B"
	}
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func ParseHumanBytes(s string) int64 {
	s = strings.TrimSpace(s)
	matches := journalValRegex.FindStringSubmatch(s)
	if len(matches) < 2 {
		return 0
	}
	val, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0
	}
	unit := ""
	if len(matches) >= 3 {
		unit = strings.ToUpper(matches[2])
	}
	multiplier := float64(1)
	switch {
	case strings.HasPrefix(unit, "K"):
		multiplier = 1024
	case strings.HasPrefix(unit, "M"):
		multiplier = 1024 * 1024
	case strings.HasPrefix(unit, "G"):
		multiplier = 1024 * 1024 * 1024
	case strings.HasPrefix(unit, "T"):
		multiplier = 1024 * 1024 * 1024 * 1024
	}
	return int64(val * multiplier)
}

func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// IsDatabaseOrProtectedFile checks if a path belongs to a database storage engine or persistent state.
func IsDatabaseOrProtectedFile(filePath string) bool {
	cleaned := path.Clean(strings.TrimSpace(filePath))
	lower := strings.ToLower(cleaned)
	if strings.Contains(lower, "galera.cache") || strings.Contains(lower, "grastate.dat") || strings.Contains(lower, "gvwstate.dat") {
		return true
	}
	// Database engine directories
	if strings.Contains(lower, "/mysql/") || strings.Contains(lower, "/mariadb/") ||
		strings.Contains(lower, "/postgresql/") || strings.Contains(lower, "/postgres/") ||
		strings.Contains(lower, "/redis/") || strings.Contains(lower, "/mongodb/") ||
		strings.Contains(lower, "/docker/volumes/") || strings.Contains(lower, "/containerd/") {
		return true
	}
	// Database engine file extensions and disk images
	exts := []string{
		".ibd", ".frm", ".myd", ".myi", ".db", ".sqlite", ".sqlite3",
		".rdb", ".aof", ".wal", ".mdf", ".ldf", ".qcow2", ".vmdk", ".img", ".raw",
		".dbf", ".wt", ".bson",
	}
	if strings.Contains(lower, "ibdata") {
		return true
	}
	for _, ext := range exts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// IsLogOrDisposableFile checks if a path is a recognizable log or disposable text file.
func IsLogOrDisposableFile(filePath string) bool {
	cleaned := path.Clean(strings.TrimSpace(filePath))
	if IsDatabaseOrProtectedFile(cleaned) {
		return false
	}
	lower := strings.ToLower(cleaned)
	if strings.HasSuffix(lower, ".log") || strings.HasSuffix(lower, ".txt") ||
		strings.HasSuffix(lower, ".out") || strings.HasSuffix(lower, ".err") ||
		strings.HasSuffix(lower, ".dump") {
		return true
	}
	if strings.HasPrefix(lower, "/var/log/") || strings.HasPrefix(lower, "/tmp/") || strings.HasPrefix(lower, "/var/tmp/") {
		return true
	}
	return false
}

const TriageProbeScript = `TARGET_SHELL="sh"
if command -v bash >/dev/null 2>&1; then
    TARGET_SHELL="bash"
fi
$TARGET_SHELL -s << 'PANTAU_TRIAGE_EOF'
if [ -n "$ZSH_VERSION" ]; then
    setopt SH_WORD_SPLIT 2>/dev/null
fi

SUDO=""
if [ "$(id -u)" != "0" ]; then
    if sudo -n true 2>/dev/null; then
        SUDO="sudo -n "
    fi
fi

TIMEOUT_FAST=""
TIMEOUT_FIND=""
if command -v timeout >/dev/null 2>&1; then
    TIMEOUT_FAST="timeout 3"
    TIMEOUT_FIND="timeout 8"
fi

STDBUF=""
if command -v stdbuf >/dev/null 2>&1; then
    STDBUF="stdbuf -oL"
fi

RUN_FAST="$SUDO $TIMEOUT_FAST $STDBUF"
RUN_FIND="$SUDO $TIMEOUT_FIND $STDBUF"

echo "===SUDO_STATUS==="
echo "$SUDO"
id -u 2>/dev/null || echo "1000"

echo "===PARTITION==="
df -Pk / 2>/dev/null | tail -n 1

echo "===SPACE_HOGS==="
# ponytail: Per-directory traversal with blackhole excludes ensures a single deep directory never starves level-1 discovery.
for d in /var /tmp /opt /data /root /home /boot /etc /srv /usr; do
    if [ -d "$d" ]; then
        $RUN_FAST du -skx \
            --exclude="*netdata*" \
            --exclude="*abrt*" \
            --exclude="*/html*" \
            --exclude="*.gvfs*" \
            --exclude="*overlay2*" \
            --exclude="*containerd*" \
            --exclude="*/.cache*" \
            --exclude="*/.npm*" \
            --exclude="*/go/pkg/mod*" \
            --exclude="*/.cargo/registry*" \
            "$d" 2>/dev/null || true
    fi
done | sort -rn | head -n 12

echo "===TOP_FILES==="
# ponytail: Prune blackholes, fuse mounts, and virtual fs, streaming files >15MB directly to sort.
SEARCH_DIRS=""
for d in /var /tmp /opt /data /root /home /boot /srv; do
    [ -d "$d" ] && SEARCH_DIRS="$SEARCH_DIRS $d"
done
if find / -prune -printf "" 2>/dev/null; then
    $RUN_FIND find $SEARCH_DIRS -xdev \( -path "*/netdata" -o -path "*/abrt" -o -path "*/html" -o -path "*/proc" -o -path "*/sys" -o -path "*/dev" -o -path "*/run" -o -path "*/docker/overlay2" -o -path "*/containerd" -o -path "*/usr/share" -o -path "*/usr/include" -o -path "*/.git" -o -path "*/.gvfs" -o -path "*/.cache" \) -prune -o -type f -size +15M -printf "%s %p\n" 2>/dev/null | sort -rn | head -n 15
else
    ($RUN_FIND find $SEARCH_DIRS -xdev \( -path "*/netdata" -o -path "*/abrt" -o -path "*/html" -o -path "*/proc" -o -path "*/sys" -o -path "*/dev" -o -path "*/run" -o -path "*/docker/overlay2" -o -path "*/containerd" -o -path "*/usr/share" -o -path "*/usr/include" -o -path "*/.git" -o -path "*/.gvfs" -o -path "*/.cache" \) -prune -o -type f -size +15M -exec ls -ln {} \; 2>/dev/null || true) | awk '{size=$5; $1=$2=$3=$4=$5=$6=$7=$8=""; sub(/^[ \t]+/, ""); print size, $0}' | sort -rn | head -n 15
fi

echo "===RUNAWAY_FILES==="
# ponytail: Prune DBMS data dirs, Galera caches, and docker overlays, streaming non-db runaway logs modified in last 60m.
RUNAWAY_DIRS=""
for d in /var /tmp /opt /data /root /home; do
    [ -d "$d" ] && RUNAWAY_DIRS="$RUNAWAY_DIRS $d"
done
if find / -prune -printf "" 2>/dev/null; then
    $RUN_FIND find $RUNAWAY_DIRS -xdev \( -path "*/netdata" -o -path "*/abrt" -o -path "*/html" -o -path "*/proc" -o -path "*/sys" -o -path "*/dev" -o -path "*/run" -o -path "*/mysql" -o -path "*/mariadb" -o -path "*/postgresql" -o -path "*/postgres" -o -path "*/redis" -o -path "*/mongodb" -o -path "*/docker/volumes" -o -path "*/docker/overlay2" -o -path "*/containerd" -o -path "*/.git" -o -path "*/.gvfs" -o -path "*/.cache" \) -prune -o -type f -mmin -60 -size +15M \( -name "*.log" -o -name "*.log.*" -o -name "*.txt" -o -name "*.out" -o -name "*.err" -o -name "*.dump" -o -path "/var/log/*" -o -path "/tmp/*" -o -path "/var/tmp/*" \) ! -name "*.ibd" ! -name "*.frm" ! -name "*.myd" ! -name "*.myi" ! -name "*.db" ! -name "*.sqlite*" ! -name "*.rdb" ! -name "*.wal" ! -name "*.qcow2" ! -name "*.vmdk" ! -name "*.img" ! -name "*.dbf" ! -name "*.wt" ! -name "galera.cache" ! -name "grastate.dat" ! -name "gvwstate.dat" -printf "%s %p\n" 2>/dev/null | sort -rn | head -n 10
else
    ($RUN_FIND find $RUNAWAY_DIRS -xdev \( -path "*/netdata" -o -path "*/abrt" -o -path "*/html" -o -path "*/proc" -o -path "*/sys" -o -path "*/dev" -o -path "*/run" -o -path "*/mysql" -o -path "*/mariadb" -o -path "*/postgresql" -o -path "*/postgres" -o -path "*/redis" -o -path "*/mongodb" -o -path "*/docker/volumes" -o -path "*/docker/overlay2" -o -path "*/containerd" -o -path "*/.git" -o -path "*/.gvfs" -o -path "*/.cache" \) -prune -o -type f -mmin -60 -size +15M \( -name "*.log" -o -name "*.log.*" -o -name "*.txt" -o -name "*.out" -o -name "*.err" -o -name "*.dump" -o -path "/var/log/*" -o -path "/tmp/*" -o -path "/var/tmp/*" \) ! -name "*.ibd" ! -name "*.frm" ! -name "*.myd" ! -name "*.myi" ! -name "*.db" ! -name "*.sqlite*" ! -name "*.rdb" ! -name "*.wal" ! -name "*.qcow2" ! -name "*.vmdk" ! -name "*.img" ! -name "*.dbf" ! -name "*.wt" ! -name "galera.cache" ! -name "grastate.dat" ! -name "gvwstate.dat" -exec ls -ln {} \; 2>/dev/null || true) | awk '{size=$5; $1=$2=$3=$4=$5=$6=$7=$8=""; sub(/^[ \t]+/, ""); print size, $0}' | sort -rn | head -n 10
fi

echo "===GHOST_FILES==="
if command -v lsof >/dev/null 2>&1; then
    $SUDO lsof -b +L1 2>/dev/null | awk 'NR>1 {
        cmd = $1; pid = $2; sz = $7;
        path = "";
        for (i = 10; i <= NF; i++) {
            if ($i == "(deleted)") break;
            path = (path == "" ? $i : path " " $i);
        }
        if (sz ~ /^[0-9]+$/ && sz > 0 && path ~ /^\//) {
            print cmd, pid, sz, path;
        }
    }' | sort -k3 -rn | head -n 15
else
    for pid_dir in /proc/[0-9]*; do
        [ -d "$pid_dir/fd" ] || continue
        for fd in "$pid_dir"/fd/*; do
            tgt=$(readlink "$fd" 2>/dev/null)
            case "$tgt" in
                *"(deleted)"*)
                    clean_path="${tgt% (deleted)}"
                    sz=$(stat -L -c %s "$fd" 2>/dev/null || echo 0)
                    if [ "$sz" -gt 0 ] 2>/dev/null; then
                        pid="${pid_dir#/proc/}"
                        pname=$(cat "$pid_dir/comm" 2>/dev/null || echo "proc")
                        echo "$pname $pid $sz $clean_path"
                    fi
                    ;;
            esac
        done
    done 2>/dev/null | sort -k3 -rn | head -n 15
fi

echo "===RECLAIM_JOURNAL==="
if command -v journalctl >/dev/null 2>&1; then
    $SUDO journalctl --disk-usage 2>/dev/null || echo "0"
else
    echo "0"
fi

echo "===RECLAIM_PKGCACHE==="
if [ -d /var/cache/apt/archives ]; then
    echo "apt"
    $SUDO du -sk /var/cache/apt/archives 2>/dev/null | awk '{print $1*1024}'
elif [ -d /var/cache/dnf ] || [ -d /var/cache/yum ]; then
    echo "dnf"
    $SUDO du -sk /var/cache/dnf /var/cache/yum 2>/dev/null | awk '{sum+=$1} END {print sum*1024}'
elif [ -d /var/cache/pacman/pkg ]; then
    echo "pacman"
    $SUDO du -sk /var/cache/pacman/pkg 2>/dev/null | awk '{print $1*1024}'
elif [ -d /var/cache/apk ]; then
    echo "apk"
    $SUDO du -sk /var/cache/apk 2>/dev/null | awk '{print $1*1024}'
else
    echo "none"
    echo "0"
fi

echo "===RECLAIM_ROTATED_LOGS==="
$SUDO find /var/log -type f \( -name "*.gz" -o -name "*.[0-9].gz" -o -name "*.1" \) -exec ls -ln {} + 2>/dev/null | awk '{sum+=$5} END {print sum+0}'

echo "===RECLAIM_COREDUMP==="
if [ -d /var/lib/systemd/coredump ]; then
    $SUDO du -sk /var/lib/systemd/coredump 2>/dev/null | awk '{print $1*1024}'
else
    echo "0"
fi

echo "===RECLAIM_DOCKER==="
if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
    echo "active"
    docker system df --format '{{.Reclaimable}}' 2>/dev/null || echo "0B"
else
    echo "none"
fi
PANTAU_TRIAGE_EOF
`

// CancelTriage attempts to abort an in-flight probe on the target host and proactively kill remote triage processes.
func (s *Service) CancelTriage(hostID int64) bool {
	val, ok := s.inFlight.Load(hostID)
	if !ok {
		return false
	}
	if cancel, isCancel := val.(context.CancelFunc); isCancel && cancel != nil {
		cancel()
	}

	// ponytail: Proactively fire an SSH kill command to terminate any lingering du/find processes on the host.
	go func() {
		if s.inspector == nil || s.db == nil {
			return
		}
		h, err := s.db.GetHost(hostID)
		if err != nil || h == nil {
			return
		}
		runner, err := s.inspector.GetRunnerForHost(h)
		if err != nil {
			return
		}
		defer runner.Close()
		killScript := `TARGET_SHELL="sh"
if command -v bash >/dev/null 2>&1; then
    TARGET_SHELL="bash"
fi
$TARGET_SHELL -s << 'PANTAU_KILL_EOF'
SUDO=""
if [ "$(id -u)" != "0" ]; then
    if sudo -n true 2>/dev/null; then
        SUDO="sudo -n "
    fi
fi
$SUDO pkill -9 -f "du -skx" 2>/dev/null || true
$SUDO pkill -9 -f "find /" 2>/dev/null || true
PANTAU_KILL_EOF
`
		_, _, _, _ = runner.ExecTimeout(killScript, 5*time.Second)
	}()

	return true
}

func parseCPULoad1m(raw string) float64 {
	if raw == "" {
		return 0
	}
	parts := strings.Split(raw, ",")
	if len(parts) == 0 {
		return 0
	}
	val, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return 0
	}
	return val
}

// CheckThrottle evaluates whether an on-demand storage scan is permitted on the target host.
func (s *Service) CheckThrottle(hostID int64, force ...bool) (*ThrottleStatus, error) {
	isForce := len(force) > 0 && force[0]

	h, err := s.db.GetHost(hostID)
	if err != nil {
		return nil, fmt.Errorf("get host: %w", err)
	}
	if h == nil {
		return nil, errors.New("host not found")
	}

	load1m := parseCPULoad1m(h.CPULoad)
	cpuCores := h.CPUCores
	writeSpeed := h.DiskWriteSpeedBps

	state := LoadStateNormal
	cooldownDuration := time.Duration(CooldownNormalSeconds) * time.Second

	isCPUOverloaded := cpuCores > 0 && load1m > (2.0*float64(cpuCores))
	isWriteVeryBusy := writeSpeed >= WriteSpeedVeryBusyThresholdBps

	if isWriteVeryBusy || isCPUOverloaded {
		state = LoadStateVeryBusy
		cooldownDuration = time.Duration(CooldownVeryBusySeconds) * time.Second
	} else if writeSpeed >= WriteSpeedBusyThresholdBps {
		state = LoadStateBusy
		cooldownDuration = time.Duration(CooldownBusySeconds) * time.Second
	}

	status := &ThrottleStatus{
		State:           state,
		CooldownSeconds: int(cooldownDuration.Seconds()),
		CanForce:        true,
		WriteSpeedBps:   writeSpeed,
		CPULoad1m:       load1m,
		CPUCores:        cpuCores,
	}

	var lastTime *time.Time
	if val, ok := s.lastScanTimes.Load(hostID); ok {
		if t, okT := val.(time.Time); okT {
			lastTime = &t
			status.LastScannedAt = &t
		}
	}

	// Force scan bypasses all throttle limits
	if isForce {
		status.Allowed = true
		status.RemainingSeconds = 0
		return status, nil
	}

	// 1. Check cooldown duration from last scan
	if lastTime != nil {
		elapsed := time.Since(*lastTime)
		if elapsed < cooldownDuration {
			remaining := int((cooldownDuration - elapsed).Seconds())
			if remaining < 1 {
				remaining = 1
			}
			status.Allowed = false
			status.RemainingSeconds = remaining
			if state == LoadStateVeryBusy {
				status.Reason = fmt.Sprintf("Remote server is very busy (Disk Write: %s/s, Load: %.2f). Scan throttled to protect server health.", FormatBytes(writeSpeed), load1m)
				status.ReasonID = fmt.Sprintf("Server remote sedang sangat sibuk (Disk Write: %s/s, Load: %.2f). Pemindaian dibatasi demi keselamatan server.", FormatBytes(writeSpeed), load1m)
			} else if state == LoadStateBusy {
				status.Reason = fmt.Sprintf("Remote server is busy (Disk Write: %s/s). Cooldown active for %d seconds.", FormatBytes(writeSpeed), remaining)
				status.ReasonID = fmt.Sprintf("Server remote sedang sibuk (Disk Write: %s/s). Waktu jeda (cooldown) aktif %d detik.", FormatBytes(writeSpeed), remaining)
			} else {
				status.Reason = fmt.Sprintf("Scan cooldown active. Please wait %d seconds before rescanning.", remaining)
				status.ReasonID = fmt.Sprintf("Waktu jeda pemindaian aktif. Silakan tunggu %d detik sebelum memindai kembali.", remaining)
			}
			return status, nil
		}
	}

	// 2. If cooldown expired or never scanned, check if system is currently Very Busy
	if state == LoadStateVeryBusy {
		status.Allowed = false
		status.RemainingSeconds = 0
		status.Reason = fmt.Sprintf("Remote server is very busy (Disk Write: %s/s, Load: %.2f). Scanning disabled to protect server stability.", FormatBytes(writeSpeed), load1m)
		status.ReasonID = fmt.Sprintf("Server remote sedang sangat sibuk (Disk Write: %s/s, Load: %.2f). Pemindaian dinonaktifkan demi stabilitas server.", FormatBytes(writeSpeed), load1m)
		return status, nil
	}

	status.Allowed = true
	status.RemainingSeconds = 0
	return status, nil
}

// ResetThrottle clears the last scan time for the host.
func (s *Service) ResetThrottle(hostID int64) {
	s.lastScanTimes.Delete(hostID)
}

// SetLastScanTimeForTest overrides the last scan time for a host in unit tests.
func (s *Service) SetLastScanTimeForTest(hostID int64, t time.Time) {
	s.lastScanTimes.Store(hostID, t)
}

// GetLastResult returns the cached triage result from the most recent scan for hostID, or nil.
func (s *Service) GetLastResult(hostID int64) *TriageResult {
	if val, ok := s.lastResults.Load(hostID); ok {
		if res, okR := val.(*TriageResult); okR {
			return res
		}
	}
	return nil
}

// SetLastResultForTest stores a mock triage result for unit tests.
func (s *Service) SetLastResultForTest(hostID int64, res *TriageResult) {
	s.lastResults.Store(hostID, res)
}

// RunTriage executes an on-demand storage diagnostic probe on the target host.
// ponytail: Concurrency guard / inFlight map guarantees no parallel du thrashing on the remote host.
func (s *Service) RunTriage(ctx context.Context, hostID int64, force ...bool) (*TriageResult, error) {
	isForce := len(force) > 0 && force[0]

	// Enforce system load throttling and rate limiting
	throttle, err := s.CheckThrottle(hostID, isForce)
	if err != nil {
		return nil, err
	}
	if !throttle.Allowed && !isForce {
		return nil, &RateLimitError{Status: throttle}
	}

	h, err := s.db.GetHost(hostID)
	if err != nil {
		return nil, fmt.Errorf("get host: %w", err)
	}
	if h == nil {
		return nil, errors.New("host not found")
	}
	if strings.EqualFold(h.Status, "down") {
		return nil, fmt.Errorf("host '%s' is offline", h.Name)
	}
	if h.OSType == "windows" {
		return nil, errors.New("storage triage is not supported on Windows hosts")
	}

	probeCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Triage Concurrency Guard with cancellation hook
	if _, loaded := s.inFlight.LoadOrStore(hostID, cancel); loaded {
		return nil, ErrBusy
	}
	defer s.inFlight.Delete(hostID)

	if s.inspector == nil {
		return nil, errors.New("inspector service not available")
	}

	runner, err := s.inspector.GetRunnerForHost(h)
	if err != nil {
		logger.Error("storagetriage", h.Name, "SSH connection failed: "+err.Error())
		return nil, fmt.Errorf("ssh connection failed: %w", err)
	}
	defer runner.Close()

	// Terminate remote runner immediately if probeCtx is cancelled
	doneChan := make(chan struct{})
	defer close(doneChan)
	go func() {
		select {
		case <-probeCtx.Done():
			_ = runner.Close()
		case <-doneChan:
		}
	}()

	start := time.Now()
	// ponytail: 50s timeout provides generous headroom for high-load disks while preventing indefinite stalls.
	stdout, _, _, err := runner.ExecTimeout(TriageProbeScript, 50*time.Second)
	scanDuration := time.Since(start).Milliseconds()
	if err != nil {
		if probeCtx.Err() == context.Canceled {
			return nil, errors.New("pemindaian dibatalkan oleh pengguna")
		}
		logger.Error("storagetriage", h.Name, "Probe execution failed: "+err.Error())
		return nil, fmt.Errorf("probe execution error: %w", err)
	}

	res := ParseTriageOutput(stdout)
	res.HostID = h.ID
	res.HostName = h.Name
	res.ScanDurationMs = scanDuration
	res.ScannedAt = time.Now().Format("2006-01-02 15:04:05")
	res.WriteRateBps = h.DiskWriteSpeedBps

	// Record successful scan time for in-memory cooldown tracking and snapshot caching
	s.lastScanTimes.Store(hostID, time.Now())
	s.lastResults.Store(hostID, res)

	return res, nil
}

// ParseTriageOutput parses the raw sectioned output from TriageProbeScript.
func ParseTriageOutput(raw string) *TriageResult {
	res := &TriageResult{
		SpaceHogs:    make([]DirectoryHog, 0),
		TopFiles:     make([]FileHog, 0),
		RunawayFiles: make([]RunawayFile, 0),
		GhostFiles:   make([]GhostFile, 0),
		Reclaimables: make([]ReclaimItem, 0),
	}

	sections := make(map[string]string)
	lines := strings.Split(raw, "\n")
	var currentSec string
	var currentLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "===SUDO_STATUS===") {
			if currentSec != "" {
				sections[currentSec] = strings.Join(currentLines, "\n")
			}
			currentSec = "SUDO_STATUS"
			currentLines = nil
		} else if strings.HasPrefix(trimmed, "===PARTITION===") {
			if currentSec != "" {
				sections[currentSec] = strings.Join(currentLines, "\n")
			}
			currentSec = "PARTITION"
			currentLines = nil
		} else if strings.HasPrefix(trimmed, "===SPACE_HOGS===") {
			if currentSec != "" {
				sections[currentSec] = strings.Join(currentLines, "\n")
			}
			currentSec = "SPACE_HOGS"
			currentLines = nil
		} else if strings.HasPrefix(trimmed, "===TOP_FILES===") {
			if currentSec != "" {
				sections[currentSec] = strings.Join(currentLines, "\n")
			}
			currentSec = "TOP_FILES"
			currentLines = nil
		} else if strings.HasPrefix(trimmed, "===RUNAWAY_FILES===") {
			if currentSec != "" {
				sections[currentSec] = strings.Join(currentLines, "\n")
			}
			currentSec = "RUNAWAY_FILES"
			currentLines = nil
		} else if strings.HasPrefix(trimmed, "===GHOST_FILES===") {
			if currentSec != "" {
				sections[currentSec] = strings.Join(currentLines, "\n")
			}
			currentSec = "GHOST_FILES"
			currentLines = nil
		} else if strings.HasPrefix(trimmed, "===RECLAIM_JOURNAL===") {
			if currentSec != "" {
				sections[currentSec] = strings.Join(currentLines, "\n")
			}
			currentSec = "RECLAIM_JOURNAL"
			currentLines = nil
		} else if strings.HasPrefix(trimmed, "===RECLAIM_PKGCACHE===") {
			if currentSec != "" {
				sections[currentSec] = strings.Join(currentLines, "\n")
			}
			currentSec = "RECLAIM_PKGCACHE"
			currentLines = nil
		} else if strings.HasPrefix(trimmed, "===RECLAIM_ROTATED_LOGS===") {
			if currentSec != "" {
				sections[currentSec] = strings.Join(currentLines, "\n")
			}
			currentSec = "RECLAIM_ROTATED_LOGS"
			currentLines = nil
		} else if strings.HasPrefix(trimmed, "===RECLAIM_COREDUMP===") {
			if currentSec != "" {
				sections[currentSec] = strings.Join(currentLines, "\n")
			}
			currentSec = "RECLAIM_COREDUMP"
			currentLines = nil
		} else if strings.HasPrefix(trimmed, "===RECLAIM_DOCKER===") {
			if currentSec != "" {
				sections[currentSec] = strings.Join(currentLines, "\n")
			}
			currentSec = "RECLAIM_DOCKER"
			currentLines = nil
		} else {
			currentLines = append(currentLines, line)
		}
	}
	if currentSec != "" {
		sections[currentSec] = strings.Join(currentLines, "\n")
	}

	// 1. Sudo status & UID
	sudoSec := sections["SUDO_STATUS"]
	if strings.Contains(sudoSec, "sudo -n") {
		res.HasSudo = true
	}
	for _, l := range strings.Split(sudoSec, "\n") {
		l = strings.TrimSpace(l)
		if l == "0" {
			res.IsRoot = true
			res.HasSudo = true
			break
		}
	}
	if !res.IsRoot && !res.HasSudo {
		res.Warning = "Limited privileges: SSH user is not root and lacks sudo -n access. Protected system directories may not be fully scanned."
		res.WarningID = "Privilese terbatas: SSH user bukan root dan tidak memiliki akses sudo -n. Direktori sistem terlindung mungkin tidak terbaca seluruhnya."
	}

	// 2. Partition info
	partSec := strings.TrimSpace(sections["PARTITION"])
	if partSec != "" {
		fields := strings.Fields(partSec)
		if len(fields) >= 6 {
			res.Partition.Filesystem = fields[0]
			totalK, _ := strconv.ParseInt(fields[1], 10, 64)
			usedK, _ := strconv.ParseInt(fields[2], 10, 64)
			freeK, _ := strconv.ParseInt(fields[3], 10, 64)
			res.Partition.TotalBytes = totalK * 1024
			res.Partition.UsedBytes = usedK * 1024
			res.Partition.FreeBytes = freeK * 1024
			if res.Partition.TotalBytes > 0 {
				res.Partition.UsedPercent = (float64(res.Partition.UsedBytes) / float64(res.Partition.TotalBytes)) * 100.0
			}
			res.Partition.MountPoint = fields[5]
		}
	}

	// 3. Space Hogs (du level 1)
	hogLines := strings.Split(sections["SPACE_HOGS"], "\n")
	for _, l := range hogLines {
		fields := strings.Fields(l)
		if len(fields) >= 2 {
			sizeK, err := strconv.ParseInt(fields[0], 10, 64)
			if err == nil {
				path := strings.TrimSpace(l[len(fields[0]):])
				if path == "/" {
					continue // skip root summary line
				}
				sizeBytes := sizeK * 1024
				res.SpaceHogs = append(res.SpaceHogs, DirectoryHog{
					Path:      path,
					SizeBytes: sizeBytes,
					HumanSize: FormatBytes(sizeBytes),
				})
			}
		}
	}

	// 4. Top Files
	topFileLines := strings.Split(sections["TOP_FILES"], "\n")
	for _, l := range topFileLines {
		fields := strings.Fields(l)
		if len(fields) >= 2 {
			sizeBytes, err := strconv.ParseInt(fields[0], 10, 64)
			if err == nil {
				path := strings.TrimSpace(l[len(fields[0]):])
				res.TopFiles = append(res.TopFiles, FileHog{
					Path:      path,
					SizeBytes: sizeBytes,
					HumanSize: FormatBytes(sizeBytes),
				})
			}
		}
	}

	// 5. Runaway Files
	runawayLines := strings.Split(sections["RUNAWAY_FILES"], "\n")
	for _, l := range runawayLines {
		fields := strings.Fields(l)
		if len(fields) >= 2 {
			sizeBytes, err := strconv.ParseInt(fields[0], 10, 64)
			if err == nil {
				path := strings.TrimSpace(l[len(fields[0]):])
				// ponytail: Never classify persistent database files or binaries as runaway files.
				if IsDatabaseOrProtectedFile(path) || !IsLogOrDisposableFile(path) {
					continue
				}
				res.RunawayFiles = append(res.RunawayFiles, RunawayFile{
					Path:      path,
					SizeBytes: sizeBytes,
					HumanSize: FormatBytes(sizeBytes),
				})
			}
		}
	}

	// 6. Ghost Files
	ghostLines := strings.Split(sections["GHOST_FILES"], "\n")
	for _, l := range ghostLines {
		fields := strings.Fields(l)
		if len(fields) >= 4 {
			cmd := fields[0]
			pid := fields[1]
			sizeBytes, _ := strconv.ParseInt(fields[2], 10, 64)
			path := strings.TrimSpace(strings.Join(fields[3:], " "))
			path = strings.TrimSuffix(path, " (deleted)")
			res.GhostFiles = append(res.GhostFiles, GhostFile{
				Command:   cmd,
				PID:       pid,
				Path:      path,
				SizeBytes: sizeBytes,
				HumanSize: FormatBytes(sizeBytes),
			})
		}
	}

	// 7. Reclaimables
	var totalReclaim int64

	// 7a. Journal
	journalSec := sections["RECLAIM_JOURNAL"]
	journalBytes := ParseHumanBytes(journalSec)
	if journalBytes > 10*1024*1024 { // at least 10MB
		res.Reclaimables = append(res.Reclaimables, ReclaimItem{
			Key:            "journal",
			Name:           "Systemd Journal Logs",
			Description:    "Clean up archived systemd journal logs stored on disk",
			DescriptionID:  "Pembersihan arsip log systemd journal yang tersimpan di disk",
			EstimatedBytes: journalBytes,
			HumanSize:      FormatBytes(journalBytes),
			Command:        "journalctl --vacuum-time=2d --vacuum-size=50M",
			Available:      true,
		})
		totalReclaim += journalBytes
	}

	// 7b. Package Cache
	pkgSec := strings.Split(strings.TrimSpace(sections["RECLAIM_PKGCACHE"]), "\n")
	if len(pkgSec) >= 2 {
		pkgType := strings.TrimSpace(pkgSec[0])
		pkgBytes, _ := strconv.ParseInt(strings.TrimSpace(pkgSec[1]), 10, 64)
		if pkgBytes > 5*1024*1024 {
			cmd := "apt-get clean"
			name := "APT Package Cache"
			switch pkgType {
			case "dnf":
				cmd = "if command -v dnf >/dev/null 2>&1; then dnf clean all; else yum clean all; fi"
				name = "DNF / YUM Package Cache"
			case "pacman":
				cmd = "pacman -Scc --noconfirm"
				name = "Pacman Package Cache"
			case "apk":
				cmd = "apk cache clean"
				name = "APK Package Cache"
			}
			res.Reclaimables = append(res.Reclaimables, ReclaimItem{
				Key:            "pkgcache",
				Name:           name,
				Description:    "Clean up package installation archives (.deb/.rpm) cached by package manager",
				DescriptionID:  "Pembersihan arsip paket instalasi (.deb/.rpm) yang di-cache manajer paket",
				EstimatedBytes: pkgBytes,
				HumanSize:      FormatBytes(pkgBytes),
				Command:        cmd,
				Available:      true,
			})
			totalReclaim += pkgBytes
		}
	}

	// 7c. Rotated Logs
	logSec := strings.TrimSpace(sections["RECLAIM_ROTATED_LOGS"])
	rotBytes, _ := strconv.ParseInt(logSec, 10, 64)
	if rotBytes > 5*1024*1024 {
		res.Reclaimables = append(res.Reclaimables, ReclaimItem{
			Key:            "rotated_logs",
			Name:           "Old Rotated Logs",
			Description:    "Remove old compressed rotated log archives in /var/log (*.gz, *.1)",
			DescriptionID:  "Penghapusan arsip kompresi log rotasi lama di /var/log (*.gz, *.1)",
			EstimatedBytes: rotBytes,
			HumanSize:      FormatBytes(rotBytes),
			Command:        `find /var/log -type f \( -name "*.gz" -o -name "*.[0-9].gz" -o -name "*.1" \) -delete`,
			Available:      true,
		})
		totalReclaim += rotBytes
	}

	// 7d. Core Dumps
	coreSec := strings.TrimSpace(sections["RECLAIM_COREDUMP"])
	coreBytes, _ := strconv.ParseInt(coreSec, 10, 64)
	if coreBytes > 5*1024*1024 {
		res.Reclaimables = append(res.Reclaimables, ReclaimItem{
			Key:            "coredump",
			Name:           "Systemd Core Dumps",
			Description:    "Remove historical system crash coredump records",
			DescriptionID:  "Penghapusan rekaman crash coredump sistem lama",
			EstimatedBytes: coreBytes,
			HumanSize:      FormatBytes(coreBytes),
			Command:        "rm -f /var/lib/systemd/coredump/*",
			Available:      true,
		})
		totalReclaim += coreBytes
	}

	// 7e. Docker
	dockerSec := strings.TrimSpace(sections["RECLAIM_DOCKER"])
	if strings.HasPrefix(dockerSec, "active") {
		dockerLines := strings.Split(dockerSec, "\n")
		var dockerBytes int64
		for _, dl := range dockerLines {
			dockerBytes += ParseHumanBytes(dl)
		}
		if dockerBytes > 20*1024*1024 {
			res.Reclaimables = append(res.Reclaimables, ReclaimItem{
				Key:            "docker",
				Name:           "Docker System Prune",
				Description:    "Clean up inactive containers, dangling networks, and unused images",
				DescriptionID:  "Pembersihan kontainer nonaktif, jaringan dangling, dan image tak terpakai",
				EstimatedBytes: dockerBytes,
				HumanSize:      FormatBytes(dockerBytes),
				Command:        "docker system prune -af",
				Available:      true,
			})
			totalReclaim += dockerBytes
		}
	}

	res.TotalReclaimBytes = totalReclaim
	res.TotalReclaimHuman = FormatBytes(totalReclaim)

	return res
}

// ValidateReclaimRequest verifies and returns the safe shell command for the given action.
func ValidateReclaimRequest(req ReclaimRequest) (string, error) {
	switch req.ActionKey {
	case "journal":
		return "journalctl --vacuum-time=2d --vacuum-size=50M", nil
	case "pkgcache":
		return "if command -v apt-get >/dev/null 2>&1; then apt-get clean; elif command -v dnf >/dev/null 2>&1; then dnf clean all; elif command -v yum >/dev/null 2>&1; then yum clean all && rm -rf /var/cache/yum/*; elif command -v pacman >/dev/null 2>&1; then pacman -Scc --noconfirm; elif command -v apk >/dev/null 2>&1; then apk cache clean; fi", nil
	case "rotated_logs":
		return `find /var/log -type f \( -name "*.gz" -o -name "*.[0-9].gz" -o -name "*.1" \) -delete`, nil
	case "coredump":
		return "rm -f /var/lib/systemd/coredump/*", nil
	case "docker":
		return "docker system prune -af", nil
	case "truncate":
		target := path.Clean(strings.TrimSpace(req.TargetPath))
		if target == "" || target == "." {
			return "", errors.New("target path required for log truncation")
		}
		if !strings.HasPrefix(target, "/") {
			return "", errors.New("target path must be an absolute path")
		}
		// Security boundary: prevent truncating critical system directories or files
		lower := strings.ToLower(target)
		if lower == "/" || strings.HasPrefix(lower, "/etc/") || lower == "/etc" ||
			strings.HasPrefix(lower, "/bin/") || lower == "/bin" ||
			strings.HasPrefix(lower, "/sbin/") || lower == "/sbin" ||
			strings.HasPrefix(lower, "/usr/") || lower == "/usr" ||
			strings.HasPrefix(lower, "/boot/") || lower == "/boot" ||
			strings.HasPrefix(lower, "/dev/") || lower == "/dev" ||
			strings.HasPrefix(lower, "/proc/") || lower == "/proc" ||
			strings.HasPrefix(lower, "/sys/") || lower == "/sys" {
			return "", fmt.Errorf("path '%s' is in protected system territory and cannot be truncated", target)
		}
		// Security boundary: prevent truncating database files or persistent state
		if IsDatabaseOrProtectedFile(target) {
			return "", fmt.Errorf("path '%s' is a critical database or persistent storage file and CANNOT be truncated", target)
		}
		// Security boundary: ensure file is recognizably a log or disposable text dump
		if !IsLogOrDisposableFile(target) {
			return "", fmt.Errorf("path '%s' is not a disposable log file; truncation is strictly restricted to logs and temporary text files", target)
		}
		return fmt.Sprintf("truncate -s 0 %s", ShellQuote(target)), nil
	case "reload_service":
		unit := strings.TrimSpace(req.TargetPath)
		if unit == "" {
			return "", errors.New("systemd unit name required for service reload")
		}
		if !safeUnitRegex.MatchString(unit) {
			return "", fmt.Errorf("invalid unit name '%s'", unit)
		}
		return fmt.Sprintf("systemctl reload %s", ShellQuote(unit)), nil
	default:
		return "", fmt.Errorf("unsupported reclaim action: %s", req.ActionKey)
	}
}

// ExecuteReclaim runs an approved cleanup or remediation action.
func (s *Service) ExecuteReclaim(ctx context.Context, hostID int64, req ReclaimRequest) (*ReclaimResult, error) {
	cmd, err := ValidateReclaimRequest(req)
	if err != nil {
		return nil, err
	}

	h, err := s.db.GetHost(hostID)
	if err != nil {
		return nil, fmt.Errorf("get host: %w", err)
	}
	if h == nil {
		return nil, errors.New("host not found")
	}
	if strings.EqualFold(h.Status, "down") {
		return nil, fmt.Errorf("host '%s' is offline", h.Name)
	}
	if h.OSType == "windows" {
		return nil, errors.New("storage triage is not supported on Windows hosts")
	}

	if s.inspector == nil {
		return nil, errors.New("inspector service not available")
	}

	runner, err := s.inspector.GetRunnerForHost(h)
	if err != nil {
		return nil, fmt.Errorf("ssh connection failed: %w", err)
	}
	defer runner.Close()

	// ponytail: wrap compound command via sh -c. If sudo_password is provided, pipe securely via sudo -S.
	var fullCmd string
	if strings.TrimSpace(req.SudoPassword) != "" {
		fullCmd = fmt.Sprintf(`printf '%%s\n' %s | sudo -S -k -p '' sh -c %s 2>&1`, ShellQuote(req.SudoPassword), ShellQuote(cmd))
	} else {
		fullCmd = fmt.Sprintf(`if [ "$(id -u)" != "0" ] && sudo -n true 2>/dev/null; then
    sudo -n sh -c %s 2>&1
else
    sh -c %s 2>&1
fi`, ShellQuote(cmd), ShellQuote(cmd))
	}

	stdout, stderr, exitCode, err := runner.ExecTimeout(fullCmd, 25*time.Second)
	combined := strings.TrimSpace(stdout + "\n" + stderr)

	// Clean any occurrences of the password from the output to prevent accidental leakage
	if strings.TrimSpace(req.SudoPassword) != "" {
		combined = strings.ReplaceAll(combined, req.SudoPassword, "******")
	}

	lower := strings.ToLower(combined)
	if err != nil || exitCode != 0 || strings.Contains(lower, "permission denied") || strings.Contains(lower, "incorrect password") || strings.Contains(lower, "sorry, try again") {
		errMsg := ""
		if strings.Contains(lower, "incorrect password") || strings.Contains(lower, "sorry, try again") {
			errMsg = "Password sudo/root salah (incorrect password). Silakan periksa kembali password akun atau root server."
		} else if strings.Contains(lower, "is not in the sudoers file") {
			errMsg = fmt.Sprintf("User '%s' tidak terdaftar dalam sudoers file di server.", h.User)
		} else if strings.Contains(lower, "permission denied") {
			errMsg = "Permission denied: user SSH tidak memiliki hak root/sudo untuk menghapus berkas sistem ini."
		} else if err != nil {
			errMsg = err.Error()
		} else {
			errMsg = fmt.Sprintf("exit code %d: %s", exitCode, combined)
		}
		return &ReclaimResult{
			ActionKey: req.ActionKey,
			Command:   cmd,
			Success:   false,
			Output:    combined,
			Error:     errMsg,
		}, nil
	}

	return &ReclaimResult{
		ActionKey: req.ActionKey,
		Command:   cmd,
		Success:   true,
		Output:    combined,
	}, nil
}
