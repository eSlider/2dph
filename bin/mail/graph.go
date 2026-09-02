//usr/bin/env bash -c 'exec "${0%/*}/../cgo/zig" go run -tags=system_ladybug,mail_graph "$0" "$@"' "$0" "$@"; exit
//go:build cgo && system_ladybug && mail_graph
//
// bin/mail/graph.go - import a gator kind=mail channel into the Ladybug graph
// as Message/Person nodes + SENT/TO/CC/BCC/REPLY_TO edges (D-1.3 #260).
//
//	./bin/mail/graph.go --channel wheregroup --dry-run          # read+map+count, write nothing
//	./bin/mail/graph.go --channel wheregroup --commit           # MERGE into <root>/var/kb.lbug
//	./bin/mail/graph.go --channel wheregroup --commit --db /x/kb.lbug --skip-existing
//
// Reads ONLY the gator canon parquet/mail (source=mail/channel=<ch>/dt=*/),
// never the raw var/mail corpus. Dry-run by default; --commit writes
// idempotently (MERGE by message_id/email, D-1.2 #259) — a repeated run
// creates zero duplicates. Live-holder guard: refuses to write while the db
// file is open by a live process / brain API unless --force.
//
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	lbug "github.com/LadybugDB/go-ladybug"

	"github.com/eSlider/2dph/internal/brain"
	"github.com/eSlider/2dph/internal/config"
	"github.com/eSlider/2dph/internal/mailgraph"
	cliparse "github.com/eSlider/2dph/pkg/cli"
	"github.com/eSlider/2dph/pkg/duckdb"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

type graphFlags struct {
	channel, db, hive string
	dryRun, commit    bool
	skipExisting      bool
	force             bool
	jsonOut           bool
	batch             int
}

// gitRepoRoot resolves the actual repository checkout (KB_ROOT may point the
// db at arbitrary roots for tests).
func gitRepoRoot() string {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// hiveRoot: флаг --hive → config gator.mailhive → env GATOR_MAIL_HIVE.
func hiveRoot(cfg *config.Config, flagVal string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}
	if cfg.Gator.MailHive != "" {
		return cfg.Gator.MailHive, nil
	}
	if v := os.Getenv("GATOR_MAIL_HIVE"); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("gator mail hive root is not configured: pass --hive, set config gator.mailhive, or GATOR_MAIL_HIVE")
}

// countQuery исполняет Cypher вида MATCH ... RETURN count(...) и возвращает
// первое скалярное значение.
func countQuery(conn *lbug.Connection, q string) (int, error) {
	res, err := conn.Query(q)
	if err != nil {
		return 0, err
	}
	defer res.Close()
	for res.HasNext() {
		row, err := res.Next()
		if err != nil {
			return 0, err
		}
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 1 {
			return 0, fmt.Errorf("count row: %v", err)
		}
		n, err := strconv.Atoi(fmt.Sprint(vals[0]))
		if err != nil {
			return 0, fmt.Errorf("count value %v: %w", vals[0], err)
		}
		return n, nil
	}
	return 0, nil
}

// existingMessageIDs собирает id уже импортированных Message-узлов
// (инкрементальный sync --skip-existing).
func existingMessageIDs(conn *lbug.Connection) (map[string]bool, error) {
	res, err := conn.Query("MATCH (m:Message) RETURN m.id")
	if err != nil {
		return nil, err
	}
	defer res.Close()
	out := map[string]bool{}
	for res.HasNext() {
		row, err := res.Next()
		if err != nil {
			return nil, err
		}
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 1 {
			return nil, fmt.Errorf("message id row: %v", err)
		}
		out[fmt.Sprint(vals[0])] = true
	}
	return out, nil
}

func run(args []string) int {
	var v graphFlags
	p := cliparse.New("mail-graph")
	p.String(&v.channel, "", "channel", "gator mail channel to import (wheregroup/gmail)")
	p.Bool(&v.dryRun, "", "dry-run", "read+map+count only, write nothing")
	p.Bool(&v.commit, "", "commit", "write Message/Person nodes + edges into kb.lbug")
	p.Bool(&v.skipExisting, "", "skip-existing", "incremental: skip message_ids already in the graph")
	p.String(&v.db, "", "db", "path to kb.lbug (default <root>/var/kb.lbug)")
	p.String(&v.hive, "", "hive", "gator parquet/mail hive root (default config gator.mailhive / GATOR_MAIL_HIVE)")
	p.Bool(&v.force, "", "force", "write even if the db is open by a live process")
	p.Bool(&v.jsonOut, "", "json", "JSON result")
	p.Int(&v.batch, "", "batch", "messages per transaction (default 500)")
	if err := cliparse.Parse(p, args); err != nil {
		return cliparse.Fail(err)
	}
	if v.channel == "" {
		fmt.Fprintln(os.Stderr, "mail-graph: --channel is required (wheregroup|gmail)")
		return 2
	}
	if v.commit && v.dryRun {
		fmt.Fprintln(os.Stderr, "mail-graph: --dry-run and --commit are mutually exclusive")
		return 2
	}

	cfg, err := config.Load(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "mail-graph: config: %v\n", err)
		return 1
	}
	hive, err := hiveRoot(cfg, v.hive)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mail-graph:", err)
		return 1
	}
	glob := filepath.Join(hive, "source=mail", "channel="+v.channel, "dt=*", "*.parquet")

	// Read + map (только канон gator kind=mail; dry-run дефолт).
	ctx := context.Background()
	start := time.Now()
	raw, err := duckdb.QueryRows(ctx, mailgraph.ReadSQL(glob, v.channel))
	if err != nil {
		fmt.Fprintf(os.Stderr, "mail-graph: read parquet: %v\n", err)
		return 1
	}
	rows, skipped, firstErr := mailgraph.MapRows(raw)
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "mail-graph: warning: %d row(s) skipped (%v)\n", skipped, firstErr)
	}
	mailgraph.ResolveThreads(rows)
	mailgraph.SortByDate(rows)
	st := mailgraph.ComputeStats(rows)
	inputs := mailgraph.ToInputs(rows)

	readMs := time.Since(start).Milliseconds()
	res := map[string]any{
		"channel":  v.channel,
		"read_ms":  readMs,
		"total":    st.Total,
		"skipped":  skipped,
		"byFolder": st.ByFolder,
		"withReply": st.WithReply,
		"dangling": st.Dangling,
		"emails":   st.Emails,
		"inputs":   len(inputs),
	}

	if !v.commit { // dry-run по умолчанию
		if v.jsonOut {
			enc := json.NewEncoder(os.Stdout)
			_ = enc.Encode(res)
		} else {
			fmt.Printf("mail-graph: dry-run channel=%s: %d messages (%d inputs, %d skipped), %d unique emails\n",
				v.channel, st.Total, len(inputs), skipped, st.Emails)
			for _, f := range st.ByFolder {
				fmt.Printf("  folder %-16s %d\n", f.Folder, f.N)
			}
			fmt.Printf("  REPLY_TO: %d messages with in_reply_to, %d parents outside channel\n", st.WithReply, st.Dangling)
			fmt.Printf("  read %dms\n", readMs)
		}
		return 0
	}

	// Commit: запись в kb.lbug (MERGE, идемпотентно).
	root := cfg.Root
	if root == "" {
		root = gitRepoRoot()
	}
	dbpath := v.db
	if dbpath == "" {
		dbpath = filepath.Join(root, "var", "kb.lbug")
	}
	if err := ensureWritable(dbpath, root, cfg, v.force); err != nil {
		fmt.Fprintf(os.Stderr, "mail-graph: %v\n", err)
		return 2
	}

	db, conn, err := brain.OpenWritable(dbpath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mail-graph: open %s: %v\n", dbpath, err)
		return 1
	}
	defer db.Close()
	defer conn.Close()
	if err := brain.InitSchema(conn); err != nil {
		fmt.Fprintf(os.Stderr, "mail-graph: schema: %v\n", err)
		return 1
	}

	before := map[string]int{"Message": 0, "Person": 0}
	if before["Message"], err = countQuery(conn, "MATCH (m:Message) RETURN count(m)"); err != nil {
		fmt.Fprintf(os.Stderr, "mail-graph: count: %v\n", err)
		return 1
	}
	if before["Person"], err = countQuery(conn, "MATCH (p:Person) RETURN count(p)"); err != nil {
		fmt.Fprintf(os.Stderr, "mail-graph: count: %v\n", err)
		return 1
	}

	if v.skipExisting {
		have, err := existingMessageIDs(conn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mail-graph: existing ids: %v\n", err)
			return 1
		}
		kept := inputs[:0]
		for _, in := range inputs {
			if !have[in.ID] {
				kept = append(kept, in)
			}
		}
		skippedExisting := len(inputs) - len(kept)
		inputs = kept
		res["skippedExisting"] = skippedExisting
	}

	batch := v.batch
	if batch <= 0 {
		batch = 500
	}
	writeStart := time.Now()
	for i := 0; i < len(inputs); i += batch {
		end := i + batch
		if end > len(inputs) {
			end = len(inputs)
		}
		if err := brain.UpsertMessages(conn, inputs[i:end]); err != nil {
			fmt.Fprintf(os.Stderr, "mail-graph: upsert batch %d..%d: %v\n", i, end, err)
			return 1
		}
	}
	writeMs := time.Since(writeStart).Milliseconds()

	after := map[string]int{}
	for _, label := range []string{"Message", "Person"} {
		if after[label], err = countQuery(conn, "MATCH (n:"+label+") RETURN count(n)"); err != nil {
			fmt.Fprintf(os.Stderr, "mail-graph: count: %v\n", err)
			return 1
		}
	}
	rels := map[string]int{}
	for _, rel := range []string{"SENT", "TO", "CC", "BCC", "REPLY_TO"} {
		if rels[rel], err = countQuery(conn, "MATCH ()-[:"+rel+"]->() RETURN count(*)"); err != nil {
			fmt.Fprintf(os.Stderr, "mail-graph: count: %v\n", err)
			return 1
		}
	}
	res["db"] = dbpath
	res["write_ms"] = writeMs
	res["before"] = before
	res["after"] = after
	res["rels"] = rels

	if v.jsonOut {
		enc := json.NewEncoder(os.Stdout)
		_ = enc.Encode(res)
	} else {
		fmt.Printf("mail-graph: committed channel=%s into %s: %d messages written (%dms)\n",
			v.channel, dbpath, len(inputs), writeMs)
		fmt.Printf("  Message %d → %d, Person %d → %d\n", before["Message"], after["Message"], before["Person"], after["Person"])
		fmt.Printf("  edges: SENT=%d TO=%d CC=%d BCC=%d REPLY_TO=%d\n",
			rels["SENT"], rels["TO"], rels["CC"], rels["BCC"], rels["REPLY_TO"])
	}
	return 0
}

// ensureWritable — live-holder guard (как index.go --rebuild): refuse запись,
// пока kb.lbug открыт живым процессом / brain API отвечает, без --force.
func ensureWritable(dbpath, repoRoot string, cfg *config.Config, force bool) error {
	var reasons []string
	if holders, err := brain.LiveHolders(dbpath); err != nil {
		return fmt.Errorf("live-holder check: %w", err)
	} else if len(holders) > 0 {
		reasons = append(reasons, fmt.Sprintf("%s is open by %d process(es):\n  %s", dbpath, len(holders), strings.Join(holders, "\n  ")))
	}
	if repoRoot != "" && dbpath == filepath.Join(repoRoot, "var", "kb.lbug") &&
		brain.BrainAPIAlive("127.0.0.1:"+strconv.Itoa(cfg.Port)) {
		reasons = append(reasons, fmt.Sprintf("a brain API is answering on 127.0.0.1:%d (compose brain bind-mounts this db)", cfg.Port))
	}
	if len(reasons) > 0 && !force {
		return fmt.Errorf("refuse write; stop/restart the brain first (or pass --force):\n%s", strings.Join(reasons, "\n"))
	}
	return nil
}
