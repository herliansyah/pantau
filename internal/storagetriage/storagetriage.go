package storagetriage

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/logger"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

var (
	ErrBusy                = errors.New("a storage triage probe is already in progress for this host")
	safeUnitRegex          = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)
	journalValRegex        = regexp.MustCompile(`([0-9.]+)\s*([KMGTkmgt]?[Bb]?)`)
	clixmlStreamRegex      = regexp.MustCompile(`(?s)<Obj S="(?:progress|information)".*?</Obj>`)
	clixmlErrorStringRegex = regexp.MustCompile(`(?s)<S S="Error">(.*?)</S>`)
	clixmlAnyTagRegex      = regexp.MustCompile(`<[^>]+>`)
	clixmlHexEscapeRegex   = regexp.MustCompile(`_x([0-9a-fA-F]{4})_`)
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

type MonitoredItem struct {
	ID           int64   `json:"id,omitempty"`
	Path         string  `json:"path"`
	Label        string  `json:"label,omitempty"`
	Exists       bool    `json:"exists"`
	IsDir        bool    `json:"is_dir"`
	SizeBytes    int64   `json:"size_bytes"`
	HumanSize    string  `json:"human_size"`
	MaxSizeBytes int64   `json:"max_size_bytes,omitempty"`
	MaxSizeHuman string  `json:"max_size_human,omitempty"`
	UsedPercent  float64 `json:"used_percent,omitempty"`
	IsOverLimit  bool    `json:"is_over_limit,omitempty"`
	IsWarning    bool    `json:"is_warning,omitempty"`
	ModTime      string  `json:"mod_time,omitempty"`
	Error        string  `json:"error,omitempty"`
}

func (m *MonitoredItem) CalculateLimitStats() {
	if m.MaxSizeBytes > 0 {
		m.MaxSizeHuman = FormatBytes(m.MaxSizeBytes)
		if m.SizeBytes > 0 {
			m.UsedPercent = (float64(m.SizeBytes) / float64(m.MaxSizeBytes)) * 100.0
		} else {
			m.UsedPercent = 0
		}
		if m.SizeBytes > m.MaxSizeBytes {
			m.IsOverLimit = true
			m.IsWarning = true
		} else if m.UsedPercent >= 75.0 {
			m.IsOverLimit = false
			m.IsWarning = true
		} else {
			m.IsOverLimit = false
			m.IsWarning = false
		}
	} else {
		m.MaxSizeHuman = ""
		m.UsedPercent = 0
		m.IsOverLimit = false
		m.IsWarning = false
	}
}

func parseMonitoredItemLine(l string) (status string, sz int64, p string, ok bool) {
	l = strings.TrimSpace(l)
	if l == "" {
		return "", 0, "", false
	}
	delim := "\t"
	parts := strings.Split(l, "\t")
	if len(parts) < 3 {
		delim = `\t`
		parts = strings.Split(l, `\t`)
	}
	if len(parts) < 3 {
		return "", 0, "", false
	}
	status = strings.ToUpper(strings.TrimSpace(parts[0]))
	switch status {
	case "DIR", "FILE", "MISSING", "NOT_FOUND", "ERR":
	default:
		return "", 0, "", false
	}
	sz, _ = strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if sz < 0 {
		sz = 0
	}
	p = strings.TrimSpace(strings.Join(parts[2:], delim))
	if p == "" {
		return "", 0, "", false
	}
	return status, sz, p, true
}

// NormalizePathKey trims whitespace, standardizes slashes to '/', trims trailing '/', and lowercases.
func NormalizePathKey(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimRight(p, "/")
	if p == "" {
		return "/"
	}
	return strings.ToLower(p)
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
	HostID            int64           `json:"host_id"`
	HostName          string          `json:"host_name"`
	ScanDurationMs    int64           `json:"scan_duration_ms"`
	ScannedAt         string          `json:"scanned_at,omitempty"`
	IsRoot            bool            `json:"is_root"`
	HasSudo           bool            `json:"has_sudo"`
	WriteRateBps      int64           `json:"write_rate_bps"`
	Partition         PartitionInfo   `json:"partition"`
	SpaceHogs         []DirectoryHog  `json:"space_hogs"`
	TopFiles          []FileHog       `json:"top_files"`
	RunawayFiles      []RunawayFile   `json:"runaway_files"`
	GhostFiles        []GhostFile     `json:"ghost_files"`
	Reclaimables      []ReclaimItem   `json:"reclaimables"`
	MonitoredItems    []MonitoredItem `json:"monitored_items"`
	TotalReclaimBytes int64           `json:"total_reclaim_bytes"`
	TotalReclaimHuman string          `json:"total_reclaim_human"`
	Warning           string          `json:"warning,omitempty"`
	WarningID         string          `json:"warning_id,omitempty"`
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
	raw := strings.TrimSpace(filePath)
	normalized := strings.ReplaceAll(raw, "\\", "/")
	cleaned := path.Clean(normalized)
	lower := strings.ToLower(cleaned)
	if strings.Contains(lower, "galera.cache") || strings.Contains(lower, "grastate.dat") || strings.Contains(lower, "gvwstate.dat") ||
		strings.Contains(lower, "edb.log") || strings.Contains(lower, "edbtmp.log") || strings.Contains(lower, "edb.chk") {
		return true
	}
	// Database engine directories (POSIX and Windows)
	if strings.Contains(lower, "/mysql/") || strings.Contains(lower, "/mariadb/") ||
		strings.Contains(lower, "/postgresql/") || strings.Contains(lower, "/postgres/") ||
		strings.Contains(lower, "/redis/") || strings.Contains(lower, "/mongodb/") ||
		strings.Contains(lower, "/mssql/") || strings.Contains(lower, "sql server") ||
		strings.Contains(lower, "/docker/volumes/") || strings.Contains(lower, "/containerd/") {
		return true
	}
	// Database engine file extensions and disk images
	exts := []string{
		".ibd", ".frm", ".myd", ".myi", ".db", ".sqlite", ".sqlite3",
		".rdb", ".aof", ".wal", ".mdf", ".ldf", ".ndf", ".edb", ".dit",
		".qcow2", ".vmdk", ".img", ".raw", ".vhd", ".vhdx", ".avhd", ".avhdx",
		".dbf", ".wt", ".bson", ".bak",
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
	raw := strings.TrimSpace(filePath)
	normalized := strings.ReplaceAll(raw, "\\", "/")
	cleaned := path.Clean(normalized)
	if IsDatabaseOrProtectedFile(cleaned) {
		return false
	}
	lower := strings.ToLower(cleaned)

	// Never classify binaries, executables, scripts, or system libraries as disposable logs
	binaryExts := []string{
		".exe", ".dll", ".sys", ".bin", ".com", ".bat", ".cmd", ".ps1", ".vbs", ".sh",
		".so", ".dylib", ".jar", ".msi", ".iso", ".img", ".zip", ".tar", ".gz",
		".7z", ".rar", ".xz", ".bz2", ".cab",
		// Configuration and source code files are critical and never disposable
		".json", ".xml", ".yml", ".yaml", ".ini", ".conf", ".config", ".cfg", ".cnf",
		".py", ".js", ".ts", ".php", ".cs", ".go", ".java", ".c", ".cpp", ".h",
	}
	for _, bext := range binaryExts {
		if strings.HasSuffix(lower, bext) {
			return false
		}
	}

	if strings.HasSuffix(lower, ".log") || strings.HasSuffix(lower, ".txt") ||
		strings.HasSuffix(lower, ".out") || strings.HasSuffix(lower, ".err") ||
		strings.HasSuffix(lower, ".dump") || strings.HasSuffix(lower, ".etl") ||
		strings.Contains(lower, ".log.") {
		return true
	}
	if strings.HasPrefix(lower, "/var/log/") || strings.HasPrefix(lower, "/tmp/") || strings.HasPrefix(lower, "/var/tmp/") ||
		strings.Contains(lower, "/temp/") || strings.Contains(lower, "/logs/") {
		return true
	}
	return false
}

// WindowsTriageProbeScript is the PowerShell probe script to collect partition metrics, space hogs, large files, runaway logs, and reclaimable caches on Windows.
// ponytail: Compact single-shot script kept under 3000 chars raw (well below Windows cmd.exe 8191 limit when Base64 encoded).
const WindowsTriageProbeScript = `$ProgressPreference=0;$PID>"$env:TEMP\pantau_triage.pid";$td="$env:TEMP\pn_$PID";if(!(Test-Path -Lit $td)){md $td -ea 0|Out-Null};
"===SUDO_STATUS===";if(([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole(544)){"admin` + "`" + `n0"}else{"user` + "`" + `n1000"};
"===PARTITION===";$d=gcim Win32_LogicalDisk -f "DriveType=3" -ea 0|sort -d {$_.DeviceID -eq "C:"}|select -f 1;
if(!$d){$i=[IO.DriveInfo]::GetDrives()[0];$d=@{DeviceID=$i.Name.TrimEnd("\");Size=$i.TotalSize;FreeSpace=$i.TotalFreeSpace;FileSystem=$i.DriveFormat}};
if($d){$t=[int64]($d.Size/1KB);$f=[int64]($d.FreeSpace/1KB);$u=$t-$f;$p=if($t){[int]($u/$t*100)}else{0};$fs=if($d.FileSystem){$d.FileSystem}else{"NTFS"};"$fs $t $u $f ${p}% $($d.DeviceID)"};
"===SPACE_HOGS===";$sd=if($d.DeviceID){$d.DeviceID}else{"C:"};$w=if($env:SystemRoot){$env:SystemRoot}else{"$sd\Windows"};
filter _sz{try{$r=robocopy $_ $td /L /E /BYTES /NFL /NDL /NJH /NP /XJ /LEV:3 /MT:16 /R:0 /W:0 2>$null;if(($r-join"` + "`" + `n")-match"Bytes\s*:\s*(\d+)"){$matches[1]}else{[int64](gci -Lit $_ -Rec -Depth 2 -File -ea 0|measure -s Length).Sum}}catch{0}}
@("Windows","Program Files","Program Files (x86)","Users","ProgramData","inetpub")+(gci -Lit "$sd\" -Dir -ea 0|?{$_.Name -notmatch "^(\$|Sys|Rec)"}|select -Exp Name)|select -u|%{
$p=Join-Path "$sd\" $_;if(Test-Path -Lit $p){$s=$p|_sz;if($s){"$([int64]($s/1KB))` + "`" + `t$p"}}
};
"===TOP_FILES===";$sc=(Get-Date).AddMinutes(-60);$tf=@();$rf=@();
@("$sd\Users","$sd\ProgramData","$sd\inetpub","$w\Temp",$env:TEMP)|?{$_ -and (Test-Path -Lit $_)}|%{
$r=$_;try{robocopy $r $td /L /E /MIN:15728640 /BYTES /FP /NDL /NJH /NJS /XJ /LEV:4 /MT:16 /R:0 /W:0 2>$null|?{$_ -match "\s+(\d+)\s+([A-Za-z]:\\.+)"}|%{
$o=@{l=[int64]$matches[1];p=$matches[2].Trim()};$tf+=$o;
if($o.p -match "\.(log|txt|out|err|dump|etl)$" -and (gi -Lit $o.p -ea 0).LastWriteTime -ge $sc){$rf+=$o}
}}catch{}
};
$tf|sort p -u|sort l -desc|select -f 15|%{"$($_.l) $($_.p)"};
"===RUNAWAY_FILES===";$rf|sort p -u|sort l -desc|select -f 10|%{"$($_.l) $($_.p)"};
"===GHOST_FILES===";
"===RECLAIM_WIN_UPDATE===";"$w\SoftwareDistribution\Download"|_sz;
"===RECLAIM_TEMP===";$ts=0;@("$w\Temp",$env:TEMP,"$sd\Temp")|?{$_ -and (Test-Path -Lit $_)}|%{$ts+=($_|_sz)};"$ts";
"===RECLAIM_DUMPS===";$ds=0;@("$env:ProgramData\Microsoft\Windows\WER","$w\Minidump","$env:LOCALAPPDATA\CrashDumps")|?{$_ -and (Test-Path -Lit $_)}|%{$ds+=($_|_sz)};
$ds+=(gi -Lit "$w\MEMORY.DMP" -ea 0).Length;"$ds";
"===RECLAIM_RECYCLEBIN===";(Join-Path "$sd\" '$Recycle.Bin')|_sz;
"===RECLAIM_IIS===";$ip="$sd\inetpub\logs\LogFiles";if(Test-Path -Lit $ip){[int64](gci -Lit $ip -Rec -Depth 3 -File -ea 0|?{$_.LastWriteTime -lt (Get-Date).AddDays(-7)}|measure -s Length).Sum}else{0};
"===RECLAIM_DOCKER===";if((gcm docker -ea 0) -and (Get-Service *docker* -ea 0|?{$_.Status -eq "Running"})){docker info 2>$null;if(!$LASTEXITCODE){"active";docker system df --format "{{.Reclaimable}}" 2>$null}else{"none"}}else{"none"};
del "$env:TEMP\pantau_triage.pid",$td -Rec -Force -ea 0`

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

	// ponytail: Proactively fire an SSH kill command to terminate any lingering du/find/robocopy processes on the host.
	go func() {
		if s.inspector == nil || s.db == nil {
			return
		}
		h, err := s.db.GetHost(hostID)
		if err != nil || h == nil {
			return
		}
		if strings.EqualFold(h.OSType, "windows") {
			s.cleanupRemoteWindowsTriage(h)
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

func (s *Service) cleanupRemoteWindowsTriage(h *store.Host) {
	if s.inspector == nil || h == nil {
		return
	}
	runner, err := s.inspector.GetRunnerForHost(h)
	if err != nil {
		return
	}
	defer runner.Close()
	killScript := `if(Test-Path "$env:TEMP\pantau_triage.pid"){$p=Get-Content "$env:TEMP\pantau_triage.pid" -ea 0;if($p){taskkill /F /T /PID $p 2>$null;Stop-Process -Id $p -Force -ea 0};Remove-Item "$env:TEMP\pantau_triage.pid" -Force -ea 0};Stop-Process -Name robocopy -Force -ea 0`
	_, _, _, _ = runner.ExecTimeout(sshrunner.PowershellEncodedCmd(killScript), 5*time.Second)
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

	isWindows := strings.EqualFold(h.OSType, "windows")
	var isCPUOverloaded bool
	if isWindows {
		// ponytail: On Windows, CPULoad is % CPU saturation (0-100%). Overload threshold is >= 90%.
		isCPUOverloaded = load1m >= 90.0
	} else {
		isCPUOverloaded = cpuCores > 0 && load1m > (2.0*float64(cpuCores))
	}
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
			loadDisplay := fmt.Sprintf("%.2f", load1m)
			if isWindows {
				loadDisplay = fmt.Sprintf("%.0f%%", load1m)
			}
			if state == LoadStateVeryBusy {
				status.Reason = fmt.Sprintf("Remote server is very busy (Disk Write: %s/s, Load: %s). Scan throttled to protect server health.", FormatBytes(writeSpeed), loadDisplay)
				status.ReasonID = fmt.Sprintf("Server remote sedang sangat sibuk (Disk Write: %s/s, Load: %s). Pemindaian dibatasi demi keselamatan server.", FormatBytes(writeSpeed), loadDisplay)
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
		loadDisplay := fmt.Sprintf("%.2f", load1m)
		if isWindows {
			loadDisplay = fmt.Sprintf("%.0f%%", load1m)
		}
		status.Reason = fmt.Sprintf("Remote server is very busy (Disk Write: %s/s, Load: %s). Scanning disabled to protect server stability.", FormatBytes(writeSpeed), loadDisplay)
		status.ReasonID = fmt.Sprintf("Server remote sedang sangat sibuk (Disk Write: %s/s, Load: %s). Pemindaian dinonaktifkan demi stabilitas server.", FormatBytes(writeSpeed), loadDisplay)
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

// buildLinuxProbeScript builds the Linux probe command, appending custom monitored targets if defined.
func (s *Service) buildLinuxProbeScript(hostID int64) string {
	if s.db == nil {
		return TriageProbeScript
	}
	monitors, err := s.db.GetStorageMonitors(hostID)
	if err != nil || len(monitors) == 0 {
		return TriageProbeScript
	}
	var sb strings.Builder
	sb.WriteString("\necho \"===MONITORED_PATHS===\"\n")
	for _, m := range monitors {
		p := ShellQuote(m.Path)
		sb.WriteString(fmt.Sprintf(`p=%s
if [ -d "$p" ]; then
    sz=$($RUN_FAST du -skx "$p" 2>/dev/null | awk '{print $1*1024}')
    printf 'DIR\t%%s\t%%s\n' "${sz:-0}" "$p"
elif [ -e "$p" ] || [ -L "$p" ]; then
    sz=$(stat -c %%s "$p" 2>/dev/null || stat -f %%z "$p" 2>/dev/null || ls -ln "$p" 2>/dev/null | awk '{print $5}')
    printf 'FILE\t%%s\t%%s\n' "${sz:-0}" "$p"
else
    printf 'MISSING\t0\t%%s\n' "$p"
fi
`, p))
	}
	return strings.Replace(TriageProbeScript, "PANTAU_TRIAGE_EOF\n", sb.String()+"PANTAU_TRIAGE_EOF\n", 1)
}

// buildWindowsProbeScript builds the Windows probe command, appending custom monitored targets if defined.
func (s *Service) buildWindowsProbeScript(hostID int64) string {
	if s.db == nil {
		return WindowsTriageProbeScript
	}
	monitors, err := s.db.GetStorageMonitors(hostID)
	if err != nil || len(monitors) == 0 {
		return WindowsTriageProbeScript
	}
	var paths []string
	for _, m := range monitors {
		paths = append(paths, fmt.Sprintf("'%s'", strings.ReplaceAll(m.Path, "'", "''")))
	}
	monScript := fmt.Sprintf("\n\"===MONITORED_PATHS===\";@(%s)|%%{$p=$_;if(Test-Path -Lit $p){if(Test-Path -Lit $p -PathType Container){$s=$p|_sz;\"DIR`t$s`t$p\"}else{$s=(gi -Lit $p -ea 0).Length;\"FILE`t$s`t$p\"}}else{\"MISSING`t0`t$p\"}};\n", strings.Join(paths, ","))
	return strings.Replace(WindowsTriageProbeScript, "del \"$env:TEMP\\pantau_triage.pid\"", monScript+"del \"$env:TEMP\\pantau_triage.pid\"", 1)
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

	isWindows := strings.EqualFold(h.OSType, "windows")
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

	if s.inspector != nil && (h.OSType == "" || strings.EqualFold(h.OSType, "auto")) {
		s.inspector.DetectHostOS(h, runner)
		isWindows = strings.EqualFold(h.OSType, "windows")
	}

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

	var probeCmd string
	if isWindows {
		probeCmd = sshrunner.PowershellEncodedCmd(s.buildWindowsProbeScript(hostID))
	} else {
		probeCmd = s.buildLinuxProbeScript(hostID)
	}

	probeTimeout := 60 * time.Second
	if isWindows {
		// ponytail: Windows multi-directory robocopy needs slightly higher timeout headroom on busy disks.
		probeTimeout = 90 * time.Second
	}

	start := time.Now()
	stdout, _, _, err := runner.ExecTimeout(probeCmd, probeTimeout)
	scanDuration := time.Since(start).Milliseconds()
	if err != nil {
		if isWindows {
			// Proactively cleanup remote Windows triage processes on error or timeout so no orphans linger
			go s.cleanupRemoteWindowsTriage(h)
		}
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

	// Attach DB metadata for manual monitored items
	if s.db != nil {
		monitors, err := s.db.GetStorageMonitors(hostID)
		if err == nil && len(monitors) > 0 {
			monMap := make(map[string]store.StorageMonitorTarget)
			for _, m := range monitors {
				norm := NormalizePathKey(m.Path)
				monMap[norm] = m
			}
			seen := make(map[string]bool)
			for i := range res.MonitoredItems {
				norm := NormalizePathKey(res.MonitoredItems[i].Path)
				if m, ok := monMap[norm]; ok {
					res.MonitoredItems[i].ID = m.ID
					res.MonitoredItems[i].Label = m.Label
					res.MonitoredItems[i].MaxSizeBytes = m.MaxSizeBytes
					res.MonitoredItems[i].CalculateLimitStats()
					seen[norm] = true
				}
			}
			for _, m := range monitors {
				norm := NormalizePathKey(m.Path)
				if !seen[norm] {
					item := MonitoredItem{
						ID:           m.ID,
						Path:         m.Path,
						Label:        m.Label,
						Exists:       false,
						HumanSize:   "Not Evaluated",
						MaxSizeBytes: m.MaxSizeBytes,
					}
					item.CalculateLimitStats()
					res.MonitoredItems = append(res.MonitoredItems, item)
					seen[norm] = true
				}
			}
		}
	}

	if isWindows && !res.IsRoot && !res.HasSudo {
		res.Warning = "Limited privileges: SSH user is not running as Administrator. Protected system directories may not be fully scanned."
		res.WarningID = "Privilese terbatas: SSH user tidak berjalan sebagai Administrator. Direktori sistem terlindung mungkin tidak terbaca seluruhnya."
	}

	// Record successful scan time for in-memory cooldown tracking and snapshot caching
	s.lastScanTimes.Store(hostID, time.Now())
	s.lastResults.Store(hostID, res)

	return res, nil
}

// ParseTriageOutput parses the raw sectioned output from TriageProbeScript.
func ParseTriageOutput(raw string) *TriageResult {
	res := &TriageResult{
		SpaceHogs:      make([]DirectoryHog, 0),
		TopFiles:       make([]FileHog, 0),
		RunawayFiles:   make([]RunawayFile, 0),
		GhostFiles:     make([]GhostFile, 0),
		Reclaimables:   make([]ReclaimItem, 0),
		MonitoredItems: make([]MonitoredItem, 0),
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
		} else if strings.HasPrefix(trimmed, "===RECLAIM_WIN_UPDATE===") {
			if currentSec != "" {
				sections[currentSec] = strings.Join(currentLines, "\n")
			}
			currentSec = "RECLAIM_WIN_UPDATE"
			currentLines = nil
		} else if strings.HasPrefix(trimmed, "===RECLAIM_TEMP===") {
			if currentSec != "" {
				sections[currentSec] = strings.Join(currentLines, "\n")
			}
			currentSec = "RECLAIM_TEMP"
			currentLines = nil
		} else if strings.HasPrefix(trimmed, "===RECLAIM_DUMPS===") {
			if currentSec != "" {
				sections[currentSec] = strings.Join(currentLines, "\n")
			}
			currentSec = "RECLAIM_DUMPS"
			currentLines = nil
		} else if strings.HasPrefix(trimmed, "===RECLAIM_RECYCLEBIN===") {
			if currentSec != "" {
				sections[currentSec] = strings.Join(currentLines, "\n")
			}
			currentSec = "RECLAIM_RECYCLEBIN"
			currentLines = nil
		} else if strings.HasPrefix(trimmed, "===RECLAIM_IIS===") {
			if currentSec != "" {
				sections[currentSec] = strings.Join(currentLines, "\n")
			}
			currentSec = "RECLAIM_IIS"
			currentLines = nil
		} else if strings.HasPrefix(trimmed, "===MONITORED_PATHS===") {
			if currentSec != "" {
				sections[currentSec] = strings.Join(currentLines, "\n")
			}
			currentSec = "MONITORED_PATHS"
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
	if strings.Contains(sudoSec, "sudo -n") || strings.Contains(sudoSec, "admin") {
		res.HasSudo = true
	}
	for _, l := range strings.Split(sudoSec, "\n") {
		l = strings.TrimSpace(l)
		if l == "0" || l == "admin" {
			res.IsRoot = true
			res.HasSudo = true
			break
		}
	}
	isWinOutput := sections["RECLAIM_WIN_UPDATE"] != "" ||
		sections["RECLAIM_IIS"] != "" ||
		sections["RECLAIM_DUMPS"] != "" ||
		sections["RECLAIM_RECYCLEBIN"] != "" ||
		strings.Contains(sections["PARTITION"], ":")

	if !res.IsRoot && !res.HasSudo {
		if isWinOutput {
			res.Warning = "Limited privileges: SSH user is not running as Administrator. Protected system directories may not be fully scanned."
			res.WarningID = "Privilese terbatas: SSH user tidak berjalan sebagai Administrator. Direktori sistem terlindung mungkin tidak terbaca seluruhnya."
		} else {
			res.Warning = "Limited privileges: SSH user is not root and lacks sudo -n access. Protected system directories may not be fully scanned."
			res.WarningID = "Privilese terbatas: SSH user bukan root dan tidak memiliki akses sudo -n. Direktori sistem terlindung mungkin tidak terbaca seluruhnya."
		}
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
	seenHogs := make(map[string]bool)
	hogLines := strings.Split(sections["SPACE_HOGS"], "\n")
	for _, l := range hogLines {
		fields := strings.Fields(l)
		if len(fields) >= 2 {
			sizeK, err := strconv.ParseInt(fields[0], 10, 64)
			if err == nil {
				path := strings.TrimSpace(l[len(fields[0]):])
				if path == "/" || strings.EqualFold(path, "C:\\") || strings.EqualFold(path, "C:") ||
					(res.Partition.MountPoint != "" && (strings.EqualFold(path, res.Partition.MountPoint) || strings.EqualFold(path, res.Partition.MountPoint+"\\"))) {
					continue // skip root summary line
				}
				normKey := strings.ToLower(strings.ReplaceAll(path, "\\", "/"))
				if seenHogs[normKey] {
					continue
				}
				seenHogs[normKey] = true
				sizeBytes := sizeK * 1024
				if sizeBytes <= 0 {
					continue
				}
				res.SpaceHogs = append(res.SpaceHogs, DirectoryHog{
					Path:      path,
					SizeBytes: sizeBytes,
					HumanSize: FormatBytes(sizeBytes),
				})
			}
		}
	}
	sort.Slice(res.SpaceHogs, func(i, j int) bool {
		return res.SpaceHogs[i].SizeBytes > res.SpaceHogs[j].SizeBytes
	})

	// 4. Top Files
	seenTop := make(map[string]bool)
	topFileLines := strings.Split(sections["TOP_FILES"], "\n")
	for _, l := range topFileLines {
		fields := strings.Fields(l)
		if len(fields) >= 2 {
			sizeBytes, err := strconv.ParseInt(fields[0], 10, 64)
			if err == nil {
				path := strings.TrimSpace(l[len(fields[0]):])
				normKey := strings.ToLower(strings.ReplaceAll(path, "\\", "/"))
				if seenTop[normKey] {
					continue
				}
				seenTop[normKey] = true
				res.TopFiles = append(res.TopFiles, FileHog{
					Path:      path,
					SizeBytes: sizeBytes,
					HumanSize: FormatBytes(sizeBytes),
				})
			}
		}
	}

	// 5. Runaway Files
	seenRunaway := make(map[string]bool)
	runawayLines := strings.Split(sections["RUNAWAY_FILES"], "\n")
	for _, l := range runawayLines {
		fields := strings.Fields(l)
		if len(fields) >= 2 {
			sizeBytes, err := strconv.ParseInt(fields[0], 10, 64)
			if err == nil {
				path := strings.TrimSpace(l[len(fields[0]):])
				normKey := strings.ToLower(strings.ReplaceAll(path, "\\", "/"))
				if seenRunaway[normKey] {
					continue
				}
				// ponytail: Never classify persistent database files or binaries as runaway files.
				if IsDatabaseOrProtectedFile(path) || !IsLogOrDisposableFile(path) {
					continue
				}
				seenRunaway[normKey] = true
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
				NameID:         "Pembersihan Sistem Docker (Prune)",
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

	// 7f. Windows Update Cache
	winUpdateSec := strings.TrimSpace(sections["RECLAIM_WIN_UPDATE"])
	if winUpdateSec != "" {
		wuBytes, _ := strconv.ParseInt(winUpdateSec, 10, 64)
		if wuBytes > 5*1024*1024 {
			res.Reclaimables = append(res.Reclaimables, ReclaimItem{
				Key:            "win_update_cache",
				Name:           "Windows Update Cache",
				NameID:         "Cache Unduhan Windows Update",
				Description:    `Clean up downloaded Windows Update installation files in SoftwareDistribution\Download`,
				DescriptionID:  `Pembersihan berkas instalasi unduhan Windows Update di SoftwareDistribution\Download`,
				EstimatedBytes: wuBytes,
				HumanSize:      FormatBytes(wuBytes),
				Command:        `Remove-Item -Path "$env:SystemRoot\SoftwareDistribution\Download\*" -Recurse -Force -ErrorAction SilentlyContinue`,
				Available:      true,
			})
			totalReclaim += wuBytes
		}
	}

	// 7g. Windows Temp Files
	tempSec := strings.TrimSpace(sections["RECLAIM_TEMP"])
	if tempSec != "" {
		tempBytes, _ := strconv.ParseInt(tempSec, 10, 64)
		if tempBytes > 5*1024*1024 {
			res.Reclaimables = append(res.Reclaimables, ReclaimItem{
				Key:            "win_temp",
				Name:           "System & User Temp Files",
				NameID:         "Berkas Sementara Sistem & Pengguna (Temp)",
				Description:    "Remove temporary files in Windows Temp and user Temp directories",
				DescriptionID:  "Pembersihan berkas sementara di direktori Windows Temp dan Temp pengguna",
				EstimatedBytes: tempBytes,
				HumanSize:      FormatBytes(tempBytes),
				Command:        `Get-ChildItem -Path "$env:SystemRoot\Temp", "$env:TEMP", "$env:SystemDrive\Temp" -Recurse -Force -ErrorAction SilentlyContinue | Remove-Item -Recurse -Force -ErrorAction SilentlyContinue`,
				Available:      true,
			})
			totalReclaim += tempBytes
		}
	}

	// 7h. Windows Error Reports & Dumps
	dumpSec := strings.TrimSpace(sections["RECLAIM_DUMPS"])
	if dumpSec != "" {
		dumpBytes, _ := strconv.ParseInt(dumpSec, 10, 64)
		if dumpBytes > 5*1024*1024 {
			res.Reclaimables = append(res.Reclaimables, ReclaimItem{
				Key:            "win_wer_dumps",
				Name:           "Windows Error Reports & Dumps",
				NameID:         "Laporan Error & Memory Dump Windows",
				Description:    "Remove Windows Error Reporting crash records and minidump files",
				DescriptionID:  "Pembersihan rekaman crash Windows Error Reporting dan berkas minidump",
				EstimatedBytes: dumpBytes,
				HumanSize:      FormatBytes(dumpBytes),
				Command:        `Remove-Item -Path "$env:ProgramData\Microsoft\Windows\WER\*", "$env:SystemRoot\Minidump\*", "$env:LOCALAPPDATA\CrashDumps\*", "$env:SystemRoot\MEMORY.DMP" -Recurse -Force -ErrorAction SilentlyContinue`,
				Available:      true,
			})
			totalReclaim += dumpBytes
		}
	}

	// 7i. Windows Recycle Bin
	rbSec := strings.TrimSpace(sections["RECLAIM_RECYCLEBIN"])
	if rbSec != "" {
		rbBytes, _ := strconv.ParseInt(rbSec, 10, 64)
		if rbBytes > 5*1024*1024 {
			res.Reclaimables = append(res.Reclaimables, ReclaimItem{
				Key:            "win_recycle_bin",
				Name:           "Recycle Bin",
				NameID:         "Keranjang Sampah (Recycle Bin)",
				Description:    "Empty deleted files in Recycle Bin across local drives",
				DescriptionID:  "Kosongkan berkas terhapus di Keranjang Sampah seluruh drive lokal",
				EstimatedBytes: rbBytes,
				HumanSize:      FormatBytes(rbBytes),
				Command:        "Clear-RecycleBin -Force -ErrorAction SilentlyContinue",
				Available:      true,
			})
			totalReclaim += rbBytes
		}
	}

	// 7j. Old IIS Logs
	iisSec := strings.TrimSpace(sections["RECLAIM_IIS"])
	if iisSec != "" {
		iisBytes, _ := strconv.ParseInt(iisSec, 10, 64)
		if iisBytes > 5*1024*1024 {
			res.Reclaimables = append(res.Reclaimables, ReclaimItem{
				Key:            "win_iis_logs",
				Name:           "Old IIS Web Server Logs",
				NameID:         "Log Web Server IIS Lama",
				Description:    `Remove archived IIS access logs older than 7 days in inetpub\logs`,
				DescriptionID:  `Pembersihan arsip log akses IIS yang lebih lama dari 7 hari di inetpub\logs`,
				EstimatedBytes: iisBytes,
				HumanSize:      FormatBytes(iisBytes),
				Command:        `Get-ChildItem -Path "$env:SystemDrive\inetpub\logs\LogFiles" -Recurse -File -ErrorAction SilentlyContinue | Where-Object { $_.LastWriteTime -lt (Get-Date).AddDays(-7) } | Remove-Item -Force -ErrorAction SilentlyContinue`,
				Available:      true,
			})
			totalReclaim += iisBytes
		}
	}

	// 8. Monitored Paths
	monSec := strings.TrimSpace(sections["MONITORED_PATHS"])
	if monSec != "" {
		for _, l := range strings.Split(monSec, "\n") {
			status, sizeBytes, p, ok := parseMonitoredItemLine(l)
			if !ok {
				continue
			}
			exists := status != "MISSING" && status != "NOT_FOUND" && status != "ERR"
			isDir := status == "DIR"
			humanSize := FormatBytes(sizeBytes)
			if !exists {
				humanSize = "Not Found"
			}
			res.MonitoredItems = append(res.MonitoredItems, MonitoredItem{
				Path:      p,
				Exists:    exists,
				IsDir:     isDir,
				SizeBytes: sizeBytes,
				HumanSize: humanSize,
			})
		}
	}

	res.TotalReclaimBytes = totalReclaim
	res.TotalReclaimHuman = FormatBytes(totalReclaim)

	return res
}

// ValidateReclaimRequest verifies and returns the safe shell command for the given action.
func ValidateReclaimRequest(req ReclaimRequest, osType ...string) (string, error) {
	isWinTarget := len(osType) > 0 && strings.EqualFold(osType[0], "windows")

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
	case "win_update_cache":
		return `Remove-Item -Path "$env:SystemRoot\SoftwareDistribution\Download\*" -Recurse -Force -ErrorAction SilentlyContinue`, nil
	case "win_temp":
		return `Get-ChildItem -Path "$env:SystemRoot\Temp", "$env:TEMP", "$env:SystemDrive\Temp" -Recurse -Force -ErrorAction SilentlyContinue | Remove-Item -Recurse -Force -ErrorAction SilentlyContinue`, nil
	case "win_wer_dumps":
		return `Remove-Item -Path "$env:ProgramData\Microsoft\Windows\WER\*", "$env:SystemRoot\Minidump\*", "$env:LOCALAPPDATA\CrashDumps\*", "$env:SystemRoot\MEMORY.DMP" -Recurse -Force -ErrorAction SilentlyContinue`, nil
	case "win_recycle_bin":
		return `Clear-RecycleBin -Force -ErrorAction SilentlyContinue`, nil
	case "win_iis_logs":
		return `Get-ChildItem -Path "$env:SystemDrive\inetpub\logs\LogFiles" -Recurse -File -ErrorAction SilentlyContinue | Where-Object { $_.LastWriteTime -lt (Get-Date).AddDays(-7) } | Remove-Item -Force -ErrorAction SilentlyContinue`, nil
	case "truncate":
		targetRaw := strings.TrimSpace(req.TargetPath)
		if targetRaw == "" || targetRaw == "." {
			return "", errors.New("target path required for log truncation")
		}

		isWindowsPath := isWinTarget ||
			(len(targetRaw) >= 2 && targetRaw[1] == ':') ||
			strings.Contains(targetRaw, "\\")

		if isWindowsPath {
			cleanWin := strings.ReplaceAll(targetRaw, "/", "\\")
			lower := strings.ToLower(cleanWin)

			if strings.HasSuffix(cleanWin, "\\") {
				return "", errors.New("target path cannot be a directory")
			}
			if len(cleanWin) < 3 || cleanWin[1] != ':' || cleanWin[2] != '\\' {
				return "", errors.New("target path must be an absolute Windows path (e.g. C:\\path\\file.log)")
			}
			if strings.Contains(cleanWin, "..") {
				return "", errors.New("path traversal characters are forbidden in target path")
			}
			rootPath := lower[2:]
			if len(cleanWin) <= 3 ||
				strings.HasPrefix(rootPath, "\\windows\\system32") ||
				strings.HasPrefix(rootPath, "\\windows\\syswow64") ||
				strings.HasPrefix(rootPath, "\\windows\\winsxs") ||
				strings.HasPrefix(rootPath, "\\boot") ||
				strings.HasPrefix(rootPath, "\\recovery") ||
				strings.HasPrefix(rootPath, "\\system volume information") ||
				strings.HasPrefix(rootPath, "\\program files") ||
				strings.HasPrefix(rootPath, "\\program files (x86)") ||
				strings.HasSuffix(lower, "pagefile.sys") ||
				strings.HasSuffix(lower, "swapfile.sys") ||
				strings.HasSuffix(lower, "hiberfil.sys") ||
				strings.Contains(lower, "dumpstack.log") ||
				strings.HasSuffix(lower, "memory.dmp") {
				return "", fmt.Errorf("path '%s' is in protected Windows territory and cannot be truncated", cleanWin)
			}
			if IsDatabaseOrProtectedFile(cleanWin) {
				return "", fmt.Errorf("path '%s' is a critical database or persistent storage file and CANNOT be truncated", cleanWin)
			}
			if !IsLogOrDisposableFile(cleanWin) {
				return "", fmt.Errorf("path '%s' is not a disposable log file; truncation is strictly restricted to logs and temporary text files", cleanWin)
			}
			psEscaped := strings.ReplaceAll(cleanWin, "'", "''")
			return fmt.Sprintf("Clear-Content -LiteralPath '%s' -Force", psEscaped), nil
		}

		// Linux target path validation
		if strings.HasSuffix(targetRaw, "/") {
			return "", errors.New("target path cannot be a directory")
		}
		target := path.Clean(targetRaw)
		if !strings.HasPrefix(target, "/") {
			return "", errors.New("target path must be an absolute path")
		}
		if strings.Contains(targetRaw, "..") {
			return "", errors.New("path traversal characters are forbidden in target path")
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
			return "", errors.New("systemd unit or Windows service name required for service reload")
		}
		if !safeUnitRegex.MatchString(unit) {
			return "", fmt.Errorf("invalid unit or service name '%s'", unit)
		}
		if isWinTarget {
			return fmt.Sprintf("Restart-Service -Name '%s' -Force", unit), nil
		}
		return fmt.Sprintf("systemctl reload %s", ShellQuote(unit)), nil
	default:
		return "", fmt.Errorf("unsupported reclaim action: %s", req.ActionKey)
	}
}

// ExecuteReclaim runs an approved cleanup or remediation action.
func (s *Service) ExecuteReclaim(ctx context.Context, hostID int64, req ReclaimRequest) (*ReclaimResult, error) {
	var isWindows bool
	var ost string
	var h *store.Host
	if s.db != nil {
		var err error
		h, err = s.db.GetHost(hostID)
		if err != nil {
			return nil, fmt.Errorf("get host: %w", err)
		}
		if h == nil {
			return nil, errors.New("host not found")
		}
		if strings.EqualFold(h.Status, "down") {
			return nil, fmt.Errorf("host '%s' is offline", h.Name)
		}
		isWindows = strings.EqualFold(h.OSType, "windows") ||
			((h.OSType == "" || strings.EqualFold(h.OSType, "auto")) &&
				((len(req.TargetPath) >= 2 && req.TargetPath[1] == ':') ||
					strings.Contains(req.TargetPath, "\\") ||
					strings.HasPrefix(req.ActionKey, "win_")))
		if isWindows {
			ost = "windows"
		}
	} else if (len(req.TargetPath) >= 2 && (req.TargetPath[1] == ':' || strings.Contains(req.TargetPath, "\\"))) || strings.HasPrefix(req.ActionKey, "win_") {
		isWindows = true
		ost = "windows"
	}

	cmd, err := ValidateReclaimRequest(req, ost)
	if err != nil {
		return nil, err
	}

	if s.db == nil {
		return nil, errors.New("database not available")
	}

	if s.inspector == nil {
		return nil, errors.New("inspector service not available")
	}

	runner, err := s.inspector.GetRunnerForHost(h)
	if err != nil {
		return nil, fmt.Errorf("ssh connection failed: %w", err)
	}
	defer runner.Close()

	if isWindows {
		var psWrap string
		if req.ActionKey == "truncate" {
			cleanWin := strings.ReplaceAll(strings.TrimSpace(req.TargetPath), "/", "\\")
			psEscaped := strings.ReplaceAll(cleanWin, "'", "''")
			// ponytail: Try Clear-Content; if inherited Deny ACE (e.g. McAfee/vendor dirs), grant explicit Allow to Administrators (*S-1-5-32-544); fallback to .NET stream SetLength(0) if locked.
			psWrap = fmt.Sprintf(`$ProgressPreference = 0; $ErrorActionPreference = "Stop"; $p = '%[1]s'; try { Clear-Content -LiteralPath $p -Force; exit 0 } catch { if ($_ -match 'Access.*is denied|UnauthorizedAccess') { icacls $p /grant '*S-1-5-32-544:(F)' 2>$null | Out-Null }; try { Clear-Content -LiteralPath $p -Force; exit 0 } catch { try { $s = [System.IO.File]::Open($p, [System.IO.FileMode]::Open, [System.IO.FileAccess]::Write, [System.IO.FileShare]::ReadWrite); $s.SetLength(0); $s.Close(); exit 0 } catch { [Console]::Error.WriteLine($_.Exception.Message); [Console]::Error.Flush(); exit 1 } } }`, psEscaped)
		} else {
			psWrap = fmt.Sprintf(`$ProgressPreference = 0; $ErrorActionPreference = "Stop"; try { %s; exit 0 } catch { [Console]::Error.WriteLine($_.Exception.Message); [Console]::Error.Flush(); exit 1 }`, cmd)
		}
		fullCmd := sshrunner.PowershellEncodedCmd(psWrap)
		stdout, stderr, exitCode, err := runner.ExecTimeout(fullCmd, 30*time.Second)
		stdout = cleanWindowsOutput(stdout)
		stderr = cleanWindowsOutput(stderr)
		combined := strings.TrimSpace(stdout + "\n" + stderr)
		if strings.TrimSpace(req.SudoPassword) != "" {
			combined = strings.ReplaceAll(combined, req.SudoPassword, "******")
		}
		lower := strings.ToLower(combined)
		isAccessDenied := strings.Contains(lower, "access is denied") ||
			(strings.Contains(lower, "access") && strings.Contains(lower, "is denied")) ||
			strings.Contains(lower, "unauthorizedaccessexception") ||
			strings.Contains(lower, "permissiondenied")
		isFileLocked := strings.Contains(lower, "cannot access the file") ||
			strings.Contains(lower, "being used by another process") ||
			strings.Contains(lower, "sharing violation")
		hasErr := err != nil || exitCode != 0 || isAccessDenied || isFileLocked
		if hasErr {
			errMsg := ""
			if isAccessDenied {
				errMsg = "Access Denied: Akun SSH tidak memiliki hak administrator yang cukup untuk melakukan aksi pembersihan ini."
			} else if isFileLocked {
				errMsg = "File Lock: Berkas sedang dikunci atau digunakan secara eksklusif oleh proses lain."
			} else if err != nil {
				errMsg = err.Error()
			} else if combined != "" {
				errMsg = fmt.Sprintf("exit code %d: %s", exitCode, combined)
			} else {
				errMsg = fmt.Sprintf("exit code %d", exitCode)
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

// cleanWindowsOutput strips CLIXML artifacts from PowerShell output and unescapes any embedded text.
func cleanWindowsOutput(raw string) string {
	s := strings.TrimSpace(raw)
	if !strings.Contains(s, "<Objs") && !strings.Contains(s, "#< CLIXML") {
		return s
	}

	// Strip CLIXML header if present
	if strings.HasPrefix(s, "#< CLIXML") {
		s = strings.TrimSpace(s[len("#< CLIXML"):])
	}

	// Remove progress and information stream records
	s = clixmlStreamRegex.ReplaceAllString(s, "")

	// Extract error strings if present
	var errParts []string
	matches := clixmlErrorStringRegex.FindAllStringSubmatch(s, -1)
	for _, m := range matches {
		if len(m) > 1 {
			decoded := decodeClixmlString(m[1])
			if trimmed := strings.TrimSpace(decoded); trimmed != "" {
				errParts = append(errParts, trimmed)
			}
		}
	}
	if len(errParts) > 0 {
		return strings.Join(errParts, " ")
	}

	// Remove remaining XML tags and unescape text
	s = clixmlAnyTagRegex.ReplaceAllString(s, "")
	s = decodeClixmlString(s)
	return strings.TrimSpace(s)
}

func decodeClixmlString(in string) string {
	out := clixmlHexEscapeRegex.ReplaceAllStringFunc(in, func(m string) string {
		sub := m[2 : len(m)-1]
		if r, err := strconv.ParseInt(sub, 16, 32); err == nil {
			return string(rune(r))
		}
		return m
	})
	out = strings.ReplaceAll(out, "&lt;", "<")
	out = strings.ReplaceAll(out, "&gt;", ">")
	out = strings.ReplaceAll(out, "&amp;", "&")
	out = strings.ReplaceAll(out, "&quot;", "\"")
	out = strings.ReplaceAll(out, "&apos;", "'")
	return out
}

// InspectAllMonitored queries and inspects all registered manual storage monitors for the given host.
func (s *Service) InspectAllMonitored(ctx context.Context, hostID int64) ([]MonitoredItem, error) {
	if s.db == nil {
		return []MonitoredItem{}, nil
	}
	monitors, err := s.db.GetStorageMonitors(hostID)
	if err != nil {
		return nil, fmt.Errorf("get storage monitors: %w", err)
	}
	if len(monitors) == 0 {
		return []MonitoredItem{}, nil
	}
	if s.inspector == nil {
		res := make([]MonitoredItem, len(monitors))
		for i, m := range monitors {
			res[i] = MonitoredItem{
				ID:        m.ID,
				Path:      m.Path,
				Label:     m.Label,
				Exists:    false,
				HumanSize: "N/A",
				Error:     "inspector service unavailable",
			}
		}
		return res, nil
	}
	h, err := s.db.GetHost(hostID)
	if err != nil || h == nil {
		return nil, errors.New("host not found")
	}
	if strings.EqualFold(h.Status, "down") {
		res := make([]MonitoredItem, len(monitors))
		for i, m := range monitors {
			res[i] = MonitoredItem{
				ID:        m.ID,
				Path:      m.Path,
				Label:     m.Label,
				Exists:    false,
				HumanSize: "Host Offline",
			}
		}
		return res, nil
	}
	runner, err := s.inspector.GetRunnerForHost(h)
	if err != nil {
		res := make([]MonitoredItem, len(monitors))
		for i, m := range monitors {
			res[i] = MonitoredItem{
				ID:        m.ID,
				Path:      m.Path,
				Label:     m.Label,
				Exists:    false,
				HumanSize: "SSH Error",
				Error:     err.Error(),
			}
		}
		return res, nil
	}
	defer runner.Close()

	isWindows := strings.EqualFold(h.OSType, "windows") ||
		((h.OSType == "" || strings.EqualFold(h.OSType, "auto")) && len(monitors) > 0 && ((len(monitors[0].Path) >= 2 && monitors[0].Path[1] == ':') || strings.Contains(monitors[0].Path, "\\")))

	var rawOutput string
	if isWindows {
		var paths []string
		for _, m := range monitors {
			paths = append(paths, fmt.Sprintf("'%s'", strings.ReplaceAll(m.Path, "'", "''")))
		}
		psCmd := fmt.Sprintf(`$ProgressPreference=0;$td="$env:TEMP\pn_$PID";if(!(Test-Path -Lit $td)){md $td -ea 0|Out-Null};filter _sz{try{$r=robocopy $_ $td /L /E /BYTES /NFL /NDL /NJH /NP /XJ /LEV:3 /MT:16 /R:0 /W:0 2>$null;if(($r-join"`+"`"+`n")-match"Bytes\s*:\s*(\d+)"){$matches[1]}else{[int64](gci -Lit $_ -Rec -Depth 2 -File -ea 0|measure -s Length).Sum}}catch{0}};@(%s)|%%{$p=$_;if(Test-Path -Lit $p){if(Test-Path -Lit $p -PathType Container){$s=$p|_sz;"DIR`+"`"+`t$s`+"`"+`t$p"}else{$s=(gi -Lit $p -ea 0).Length;"FILE`+"`"+`t$s`+"`"+`t$p"}}else{"MISSING`+"`"+`t0`+"`"+`t$p"}};del $td -Rec -Force -ea 0`, strings.Join(paths, ","))
		stdout, _, _, err := runner.ExecTimeout(sshrunner.PowershellEncodedCmd(psCmd), 20*time.Second)
		if err != nil {
			logger.Error("storagetriage", h.Name, "Inspect all monitored failed: "+err.Error())
		}
		rawOutput = cleanWindowsOutput(stdout)
	} else {
		var sb strings.Builder
		sb.WriteString("TARGET_SHELL=\"sh\"\nif command -v bash >/dev/null 2>&1; then TARGET_SHELL=\"bash\"; fi\n$TARGET_SHELL -s << 'PANTAU_MON_EOF'\n")
		sb.WriteString("RUN_FAST=\"\"\nif [ \"$(id -u)\" != \"0\" ] && sudo -n true 2>/dev/null; then RUN_FAST=\"sudo -n \"; fi\n")
		for _, m := range monitors {
			p := ShellQuote(m.Path)
			sb.WriteString(fmt.Sprintf(`p=%s
if [ -d "$p" ]; then
    sz=$($RUN_FAST du -skx "$p" 2>/dev/null | awk '{print $1*1024}')
    printf 'DIR\t%%s\t%%s\n' "${sz:-0}" "$p"
elif [ -e "$p" ] || [ -L "$p" ]; then
    sz=$(stat -c %%s "$p" 2>/dev/null || stat -f %%z "$p" 2>/dev/null || ls -ln "$p" 2>/dev/null | awk '{print $5}')
    printf 'FILE\t%%s\t%%s\n' "${sz:-0}" "$p"
else
    printf 'MISSING\t0\t%%s\n' "$p"
fi
`, p))
		}
		sb.WriteString("PANTAU_MON_EOF\n")
		stdout, _, _, err := runner.ExecTimeout(sb.String(), 15*time.Second)
		if err != nil {
			logger.Error("storagetriage", h.Name, "Inspect all monitored failed: "+err.Error())
		}
		rawOutput = stdout
	}

	itemsMap := make(map[string]MonitoredItem)
	lines := strings.Split(rawOutput, "\n")
	for _, l := range lines {
		status, sz, p, ok := parseMonitoredItemLine(l)
		if !ok {
			continue
		}
		norm := NormalizePathKey(p)
		exists := status != "MISSING" && status != "NOT_FOUND" && status != "ERR"
		hSize := FormatBytes(sz)
		if !exists {
			hSize = "Not Found"
		}
		itemsMap[norm] = MonitoredItem{
			Path:      p,
			Exists:    exists,
			IsDir:     status == "DIR",
			SizeBytes: sz,
			HumanSize: hSize,
		}
	}

	res := make([]MonitoredItem, len(monitors))
	for i, m := range monitors {
		norm := NormalizePathKey(m.Path)
		if item, ok := itemsMap[norm]; ok {
			item.ID = m.ID
			item.Label = m.Label
			item.MaxSizeBytes = m.MaxSizeBytes
			item.CalculateLimitStats()
			res[i] = item
		} else {
			it := MonitoredItem{
				ID:           m.ID,
				Path:         m.Path,
				Label:        m.Label,
				MaxSizeBytes: m.MaxSizeBytes,
				Exists:       false,
				HumanSize:   "Not Found",
			}
			it.CalculateLimitStats()
			res[i] = it
		}
	}

	if lastRes := s.GetLastResult(hostID); lastRes != nil {
		lastRes.MonitoredItems = res
	}

	return res, nil
}

// InspectMonitoredPath inspects a single target path on the remote host.
func (s *Service) InspectMonitoredPath(ctx context.Context, hostID int64, targetPath string, label string, maxSizeBytes ...int64) (*MonitoredItem, error) {
	targetPath = strings.TrimSpace(targetPath)
	if targetPath == "" {
		return nil, errors.New("target path required")
	}

	var limitBytes int64
	var monitorID int64
	if len(maxSizeBytes) > 0 && maxSizeBytes[0] > 0 {
		limitBytes = maxSizeBytes[0]
	}
	if s.db != nil {
		if monitors, err := s.db.GetStorageMonitors(hostID); err == nil {
			norm := NormalizePathKey(targetPath)
			for _, m := range monitors {
				if NormalizePathKey(m.Path) == norm {
					if limitBytes <= 0 {
						limitBytes = m.MaxSizeBytes
					}
					if label == "" {
						label = m.Label
					}
					monitorID = m.ID
					break
				}
			}
		}
	}

	item := &MonitoredItem{
		ID:           monitorID,
		Path:         targetPath,
		Label:        label,
		MaxSizeBytes: limitBytes,
		HumanSize:    "Not Found",
	}
	defer item.CalculateLimitStats()

	if s.db == nil || s.inspector == nil {
		return item, nil
	}

	h, err := s.db.GetHost(hostID)
	if err != nil || h == nil {
		return nil, errors.New("host not found")
	}
	if strings.EqualFold(h.Status, "down") {
		item.HumanSize = "Host Offline"
		return item, nil
	}

	runner, err := s.inspector.GetRunnerForHost(h)
	if err != nil {
		item.HumanSize = "SSH Error"
		item.Error = err.Error()
		return item, nil
	}
	defer runner.Close()

	isWindows := strings.EqualFold(h.OSType, "windows") ||
		((h.OSType == "" || strings.EqualFold(h.OSType, "auto")) && ((len(targetPath) >= 2 && targetPath[1] == ':') || strings.Contains(targetPath, "\\")))

	var stdout string
	if isWindows {
		psEscaped := strings.ReplaceAll(targetPath, "'", "''")
		psCmd := fmt.Sprintf(`$ProgressPreference=0;$td="$env:TEMP\pn_$PID";if(!(Test-Path -Lit $td)){md $td -ea 0|Out-Null};filter _sz{try{$r=robocopy $_ $td /L /E /BYTES /NFL /NDL /NJH /NP /XJ /LEV:3 /MT:16 /R:0 /W:0 2>$null;if(($r-join"`+"`"+`n")-match"Bytes\s*:\s*(\d+)"){$matches[1]}else{[int64](gci -Lit $_ -Rec -Depth 2 -File -ea 0|measure -s Length).Sum}}catch{0}};$p='%s';if(Test-Path -Lit $p){if(Test-Path -Lit $p -PathType Container){$s=$p|_sz;"DIR`+"`"+`t$s`+"`"+`t$p"}else{$s=(gi -Lit $p -ea 0).Length;"FILE`+"`"+`t$s`+"`"+`t$p"}}else{"MISSING`+"`"+`t0`+"`"+`t$p"};del $td -Rec -Force -ea 0`, psEscaped)
		out, _, _, _ := runner.ExecTimeout(sshrunner.PowershellEncodedCmd(psCmd), 15*time.Second)
		stdout = cleanWindowsOutput(out)
	} else {
		shCmd := fmt.Sprintf(`RUN_FAST=""; if [ "$(id -u)" != "0" ] && sudo -n true 2>/dev/null; then RUN_FAST="sudo -n "; fi; p=%s; if [ -d "$p" ]; then sz=$($RUN_FAST du -skx "$p" 2>/dev/null | awk '{print $1*1024}'); printf 'DIR\t%%s\t%%s\n' "${sz:-0}" "$p"; elif [ -e "$p" ] || [ -L "$p" ]; then sz=$(stat -c %%s "$p" 2>/dev/null || stat -f %%z "$p" 2>/dev/null || ls -ln "$p" 2>/dev/null | awk '{print $5}'); printf 'FILE\t%%s\t%%s\n' "${sz:-0}" "$p"; else printf 'MISSING\t0\t%%s\n' "$p"; fi`, ShellQuote(targetPath))
		out, _, _, _ := runner.ExecTimeout(shCmd, 15*time.Second)
		stdout = out
	}

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	for _, l := range lines {
		status, sz, p, ok := parseMonitoredItemLine(l)
		if !ok {
			continue
		}
		if p != "" && NormalizePathKey(p) != NormalizePathKey(targetPath) {
			continue
		}
		exists := status != "MISSING" && status != "NOT_FOUND" && status != "ERR"
		if p != "" {
			item.Path = p
		}
		item.Exists = exists
		item.IsDir = status == "DIR"
		item.SizeBytes = sz
		if exists {
			item.HumanSize = FormatBytes(sz)
		} else {
			item.HumanSize = "Not Found"
		}
		break
	}
	item.CalculateLimitStats()

	// Update cached TriageResult if exists
	if lastRes := s.GetLastResult(hostID); lastRes != nil {
		norm := NormalizePathKey(item.Path)
		found := false
		for idx := range lastRes.MonitoredItems {
			if NormalizePathKey(lastRes.MonitoredItems[idx].Path) == norm {
				lastRes.MonitoredItems[idx] = *item
				found = true
				break
			}
		}
		if !found {
			lastRes.MonitoredItems = append(lastRes.MonitoredItems, *item)
		}
	}

	return item, nil
}
