package sync

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOOCLIClientFlow(t *testing.T) {
	dir := t.TempDir()
	cli := filepath.Join(dir, "oo")
	script := `#!/usr/bin/env bash
set -euo pipefail
if [[ "$1 $2" == "mails list" ]]; then
  printf '%s\n' '[{"id":101},{"id":102}]'
  exit 0
fi
if [[ "$1 $2 $3" == "mails get 101" ]]; then
  printf '%s\n' '{"id":101,"subject":"Hi","from":"a@b.c","to":"x@y.z","receivedDate":"2026-08-19T12:00:00Z","attachments":[{"fileId":55,"fileName":"a.txt","storedName":"stored.txt","size":7,"contentType":"text/plain"}]}'
  exit 0
fi
if [[ "$1 $2 $3" == "mails download-attachment 55" ]]; then
  out=""
  for ((i=1; i<=$#; i++)); do
    if [[ "${!i}" == "--out" ]]; then
      j=$((i+1))
      out="${!j}"
    fi
  done
  printf 'payload' > "$out"
  printf '%s\n' '{"attachmentId":"55","bytes":7,"path":"ok"}'
  exit 0
fi
echo "unexpected args: $*" >&2
exit 1
`
	if err := os.WriteFile(cli, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	c := &OOCLIClient{bin: cli, folder: "inbox"}
	ctx := context.Background()
	ids, next, err := c.ListIDs(ctx, 10, 1)
	if err != nil {
		t.Fatalf("ListIDs: %v", err)
	}
	if len(ids) != 2 || ids[0] != 101 || ids[1] != 102 || next != 2 {
		t.Fatalf("ids=%v next=%d", ids, next)
	}
	msg, err := c.GetMessage(ctx, 101)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if msg.ID != "101" || msg.Subject != "Hi" || len(msg.Attachments) != 1 || msg.Attachments[0].FileID != "55" {
		t.Fatalf("msg=%+v", msg)
	}
	body, err := c.DownloadAttachment(ctx, "55")
	if err != nil {
		t.Fatalf("DownloadAttachment: %v", err)
	}
	if string(body) != "payload" {
		t.Fatalf("body=%q", body)
	}
}
