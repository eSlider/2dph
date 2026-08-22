package mailsync

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type OOCLIClient struct {
	bin    string
	folder string
}

// ooCliOverride is the typed-config OnlyOffice CLI path (internal/config
// OOCLI). Empty means "discover" (PATH / $HOME/go/bin/oo).
var ooCliOverride string

// SetOOCLI wires the typed-config OnlyOffice CLI path into mailsync.
func SetOOCLI(path string) { ooCliOverride = path }

func newOOAPI(cfg OOConfig, folderID int) (ooMailAPI, error) {
	if bin := findOOCLI(); bin != "" {
		folder := "inbox"
		if folderID > 0 {
			folder = strconv.Itoa(folderID)
		}
		return &OOCLIClient{bin: bin, folder: folder}, nil
	}
	return NewOOClient(cfg, folderID)
}

func findOOCLI() string {
	if v := strings.TrimSpace(ooCliOverride); v != "" {
		if _, err := os.Stat(v); err == nil {
			return v
		}
	}
	if v, err := exec.LookPath("oo"); err == nil {
		return v
	}
	home, _ := os.UserHomeDir()
	candidate := filepath.Join(home, "go", "bin", "oo")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}

func (o *OOCLIClient) ListIDs(ctx context.Context, maxIDs int, page int) ([]int, int, error) {
	args := []string{"mails", "list", "--folder", o.folder, "-o", "json"}
	if maxIDs > 0 {
		args = append(args, "--limit", strconv.Itoa(maxIDs))
	}
	if page > 1 && maxIDs > 0 {
		args = append(args, "--offset", strconv.Itoa((page-1)*maxIDs))
	}
	var rows []map[string]any
	if err := o.runJSON(ctx, &rows, args...); err != nil {
		return nil, 0, err
	}
	ids := make([]int, 0, len(rows))
	for _, row := range rows {
		switch v := row["id"].(type) {
		case float64:
			ids = append(ids, int(v))
		case string:
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				ids = append(ids, n)
			}
		}
	}
	return ids, page + 1, nil
}

func (o *OOCLIClient) GetMessage(ctx context.Context, id int) (*Message, error) {
	var raw ooMessage
	if err := o.runJSON(ctx, &raw, "mails", "get", strconv.Itoa(id), "-o", "json"); err != nil {
		return nil, err
	}
	return ooMessageToMessage(raw), nil
}

func (o *OOCLIClient) DownloadAttachment(ctx context.Context, fileID string) ([]byte, error) {
	tmp, err := os.CreateTemp("", "2dph-oo-attach-*")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpPath)
	if err := o.runJSON(ctx, nil, "mails", "download-attachment", fileID, "--out", tmpPath, "-o", "json"); err != nil {
		return nil, err
	}
	return os.ReadFile(tmpPath)
}

func (o *OOCLIClient) runJSON(ctx context.Context, out any, args ...string) error {
	cmd := exec.CommandContext(ctx, o.bin, args...)
	cmd.Env = os.Environ()
	data, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("oo %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(ee.Stderr)))
		}
		return fmt.Errorf("oo %s: %w", strings.Join(args, " "), err)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("oo %s: decode json: %w", strings.Join(args, " "), err)
	}
	return nil
}
