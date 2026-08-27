package bench

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// clockTicks is CLK_TCK on Linux (nearly universally 100); used to convert
// /proc/<pid>/stat jiffies to seconds.
const clockTicks = 100.0

// ProcSample is a point-in-time resource sample of one process, read from
// /proc/<pid>/stat and /proc/<pid>/status.
type ProcSample struct {
	UserJiffies uint64 `json:"user_jiffies"`
	SysJiffies  uint64 `json:"sys_jiffies"`
	RSSKB       int64  `json:"rss_kb"`
	PeakRSSKB   int64  `json:"peak_rss_kb"`
}

// Resources is the run-level resource delta of the sampled process.
type Resources struct {
	CPUUserSec  float64 `json:"cpu_user_sec"`
	CPUSysSec   float64 `json:"cpu_sys_sec"`
	RSSBeforeKB int64   `json:"rss_before_kb"`
	RSSAfterKB  int64   `json:"rss_after_kb"`
	RSSPeakKB   int64   `json:"rss_peak_kb"`
}

// SampleProc reads CPU jiffies and RSS of a process. pid 0 means the calling
// process (/proc/self). The comm field of /proc/<pid>/stat may contain spaces
// and parens, so the tail is split after the last ')'.
func SampleProc(pid int) (ProcSample, error) {
	statPath := "/proc/self/stat"
	statusPath := "/proc/self/status"
	if pid != 0 {
		statPath = "/proc/" + strconv.Itoa(pid) + "/stat"
		statusPath = "/proc/" + strconv.Itoa(pid) + "/status"
	}
	stat, err := os.ReadFile(statPath)
	if err != nil {
		return ProcSample{}, fmt.Errorf("read %s: %w", statPath, err)
	}
	// fields are 1-indexed: 3=comm, 14=utime, 15=stime. tail[0] is field 3,
	// so utime = tail[11] and stime = tail[12].
	s := string(stat)
	close := strings.LastIndex(s, ")")
	if close < 0 || close+2 >= len(s) {
		return ProcSample{}, fmt.Errorf("bad /proc stat: %s", s)
	}
	fields := strings.Fields(s[close+1:])
	if len(fields) < 13 {
		return ProcSample{}, fmt.Errorf("bad /proc stat fields: %s", s)
	}
	utime, err := strconv.ParseUint(fields[11], 10, 64) // field 14
	if err != nil {
		return ProcSample{}, fmt.Errorf("utime: %w", err)
	}
	stime, err := strconv.ParseUint(fields[12], 10, 64) // field 15
	if err != nil {
		return ProcSample{}, fmt.Errorf("stime: %w", err)
	}
	var rss, peak int64
	if status, err := os.ReadFile(statusPath); err == nil {
		for _, line := range strings.Split(string(status), "\n") {
			switch {
			case strings.HasPrefix(line, "VmRSS:"):
				rss = parseKB(line)
			case strings.HasPrefix(line, "VmHWM:"):
				peak = parseKB(line)
			}
		}
	}
	return ProcSample{UserJiffies: utime, SysJiffies: stime, RSSKB: rss, PeakRSSKB: peak}, nil
}

func parseKB(line string) int64 {
	fields := strings.Fields(line)
	if len(fields) >= 2 {
		if v, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
			return v
		}
	}
	return 0
}

// Resources computes the CPU delta and RSS delta/peak between two samples.
func (before ProcSample) Resources(after ProcSample) Resources {
	return Resources{
		CPUUserSec:  round3(float64(after.UserJiffies-before.UserJiffies) / clockTicks),
		CPUSysSec:   round3(float64(after.SysJiffies-before.SysJiffies) / clockTicks),
		RSSBeforeKB: before.RSSKB,
		RSSAfterKB:  after.RSSKB,
		RSSPeakKB:   maxInt64(after.PeakRSSKB, before.PeakRSSKB),
	}
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
