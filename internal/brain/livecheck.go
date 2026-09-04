// Live-process detection for the db file. Cgo-free so it unit-tests without
// the Ladybug library.
package brain

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// LiveHolders returns one "pid N (cmdline)" string per process currently
// holding path open. Detection is by device+inode of each /proc/<pid>/fd
// target, so holders are found across bind mounts and container PID
// namespaces: a compose brain service holding /data/var/kb.lbug is flagged
// when a host-side run checks the same file at ~/2dph/var/kb.lbug. Holders of
// a deleted inode whose path (in their own namespace) equals path are also
// reported — that state means a server is up but serving stale data.
//
// Best effort: unreadable /proc entries are skipped, and on non-Linux systems
// the result is always empty.
func LiveHolders(path string) ([]string, error) {
	if runtime.GOOS != "linux" {
		return nil, nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	var wantDev, wantIno uint64
	if fi, err := os.Stat(abs); err == nil {
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			wantDev, wantIno = st.Dev, st.Ino
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		pid := e.Name()
		if !isNumeric(pid) {
			continue
		}
		cmd := procCmdline(pid)
		fds, err := os.ReadDir("/proc/" + pid + "/fd")
		if err != nil {
			continue // kernel threads, other users, races
		}
		held := false
		for _, fd := range fds {
			link, err := os.Readlink("/proc/" + pid + "/fd/" + fd.Name())
			if err != nil {
				continue
			}
			clean := strings.TrimSuffix(link, " (deleted)")
			if clean == abs && strings.HasSuffix(link, " (deleted)") {
				held = true // stale deleted-inode holder at our path
				break
			}
			if wantIno == 0 {
				continue
			}
			if fi, err := os.Stat("/proc/" + pid + "/fd/" + fd.Name()); err == nil {
				if st, ok := fi.Sys().(*syscall.Stat_t); ok && st.Dev == wantDev && st.Ino == wantIno {
					held = true
					break
				}
			}
		}
		if held {
			out = append(out, fmt.Sprintf("pid %s (%s)", pid, cmd))
		}
	}
	return out, nil
}

// BrainAPIAlive reports whether a brain HTTP API answers /stats on addr
// (e.g. "127.0.0.1:8630"). The compose brain runs in its own container as
// another uid, so its fds are invisible to LiveHolders from the host; a
// healthy /stats response is the reliable cross-namespace signal.
func BrainAPIAlive(addr string) bool {
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + addr + "/stats")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return err == nil && strings.Contains(string(b), `"total"`)
}

func procCmdline(pid string) string {
	b, err := os.ReadFile("/proc/" + pid + "/cmdline")
	if err != nil || len(b) == 0 {
		return "?"
	}
	return strings.TrimSpace(strings.ReplaceAll(string(b), "\x00", " "))
}

func isNumeric(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}
