//usr/bin/env bash -c 'exec "${0%/*}/../cgo/zig" go run -tags=system_ladybug,brain_index "$0" "$@"' "$0" "$@"; exit
//go:build cgo && system_ladybug && brain_index
//
// bin/brain/index.go - rebuild the Ladybug graph (Go+Zig write path).
//
//	./bin/brain/index.go --rebuild --with-facts --with-chats
//	./bin/brain/index.go --rebuild --with-mail
//	./bin/brain/index.go --rebuild --git-root ~/projects
//	./bin/brain/index.go --dry-run --with-mail
//
// P-9.3: index — драйвер адаптеров корпуса (internal/corpus). Каждый корпус
// (docs/mail/chats/git) реализует contract.Source; флаги включают/выключают
// адаптеры, единый writer (brain.WriteCorpus) пишет leafs с id=ContentHash.
// facts остаются отдельным путём (не корпус, а evidence gate).
//
// Bulk mail/corpus still --rebuild (fresh file, indexes last).
// NOTE: never run gofmt -w on this file — it breaks the shebang.
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

	"github.com/eSlider/2dph/internal/brain"
	"github.com/eSlider/2dph/internal/config"
	"github.com/eSlider/2dph/internal/contract"
	"github.com/eSlider/2dph/internal/corpus"
	cliparse "github.com/eSlider/2dph/pkg/cli"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

type indexFlags struct {
	db, factsJSON, withChats, since, gitRoot                                            string
	corpus                                                                              []string
	rebuild, noDefaults, withMail, withFacts, dryRun, skipIndexes, jsonOut, skip, force bool
	limit, workers, batch, chunk, progress                                              int
}

// gitRepoRoot resolves the actual repository checkout (independent of
// KB_ROOT, which points the corpus/db at arbitrary roots for tests and
// throwaway builds).
func gitRepoRoot() string {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// sources собирает адаптеры корпуса по флагам (P-9.3): добавление/удаление
// корпуса = добавление/удаление адаптера здесь + пересборка.
func sources(v indexFlags, root string) []contract.Source {
	ss := []contract.Source{
		corpus.Docs{Root: root, ExtraDirs: v.corpus, NoDefaults: v.noDefaults},
	}
	if v.withMail {
		ss = append(ss, corpus.Mail{Root: root, Since: v.since})
	}
	if v.withChats != "" {
		ss = append(ss, corpus.Chats{Root: root, Dir: v.withChats})
	}
	if v.gitRoot != "" {
		ss = append(ss, corpus.Git{Root: v.gitRoot})
	}
	return ss
}

func run(args []string) int {
	cfg, err := config.Load(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain/index: config: %v\n", err)
		return 1
	}
	brain.Configure(cfg)

	// historic wrapper prepended --with-mail; keep default off unless passed
	v := indexFlags{}
	p := cliparse.New("brain-index")
	p.Description = "build the 2dph brain index (Go+Zig)"
	p.String(&v.db, "", "db", "path to kb.lbug")
	p.Bool(&v.rebuild, "", "rebuild", "fresh db + indexes")
	p.Bool(&v.noDefaults, "", "no-defaults", "skip README/docs/skills")
	p.Bool(&v.withMail, "", "with-mail", "include var/corpus/mail + legacy var/mail message.md leafs")
	p.Bool(&v.withFacts, "", "with-facts", "facts/extract --json --dry-run")
	p.String(&v.factsJSON, "", "facts-json", "JSON facts file")
	p.String(&v.withChats, "", "with-chats", "chat markdown dir (empty=off; bare flag via const path)")
	p.String(&v.gitRoot, "", "git-root", "dir of git repos to import into the brain")
	p.String(&v.since, "", "since", "with --with-mail, messages >= YYYY-MM-DD")
	p.Bool(&v.dryRun, "", "dry-run", "count leafs, write nothing")
	p.Bool(&v.skipIndexes, "", "skip-indexes", "write leafs only")
	p.Int(&v.limit, "", "limit", "max leafs to stream (cross-chunk dedup applied before write)")
	p.Int(&v.workers, "", "workers", "parallel embedding workers (default 4)")
	p.Int(&v.batch, "", "batch", "leafs per transaction (default 64)")
	p.Int(&v.chunk, "", "chunk", "leafs per chunk before write (default 2048)")
	p.Int(&v.progress, "", "progress", "progress/ETA line every N seconds")
	p.Bool(&v.skip, "", "skip", "skip leafs already in the db (resume)")
	p.Bool(&v.force, "", "force", "rebuild even if the db is open by a live process")
	p.Bool(&v.jsonOut, "", "json", "JSON stats")
	// --corpus may repeat: parse manually from args leftovers after flaggy
	if err := cliparse.Parse(p, filterCorpusArgs(args, &v.corpus)); err != nil {
		return cliparse.Fail(err)
	}
	// bare --with-chats without value → default dir (var/corpus/chats/md)
	if hasBareWithChats(args) && v.withChats == "" {
		v.withChats = filepath.Join(brain.RepoRoot(), "var", "corpus", "chats", "md")
	}

	root := brain.RepoRoot()
	dbpath := v.db
	if dbpath == "" {
		dbpath = filepath.Join(root, "var", "kb.lbug")
	}
	port := strconv.Itoa(cfg.Port)
	if cfg.Port <= 0 {
		port = "8630"
	}

	// Pass 1: подсчёт уникальных leafs по источникам (dry-run + сквозной
	// total/прогресс). Счёт дедуплицирован (issue #248 A1): Total = уникальных
	// ContentHash (сколько ляжет в БД), Streamed = сырой стрим (с дублями
	// live+legacy mail). --limit применяется и здесь, чтобы dry-run и
	// фактическая запись давали одно число.
	// Чанкованная запись (issue #237) не копит корпус: pass 2 стримит и пишет
	// чанками по --chunk, память ограничена размером чанка (~2048 leafs).
	ctx := context.Background()
	stats, err := brain.CountCorpus(ctx, sources(v, root), v.limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain/index: corpus: %v\n", err)
		return 1
	}

	facts := []brain.LeafInput{}
	if v.factsJSON != "" {
		raw, err := os.ReadFile(v.factsJSON)
		if err != nil {
			fmt.Fprintf(os.Stderr, "brain/index: facts-json: %v\n", err)
			return 1
		}
		facts = append(facts, factsFromJSON(raw)...)
	}
	if v.withFacts {
		facts = append(facts, factsFromExtract(root)...)
	}

	if v.dryRun {
		msg := map[string]any{
			"info": stats.Total, "facts": len(facts), "by_source": stats.BySource,
			"streamed": stats.Streamed, "would_index": true,
		}
		if v.jsonOut {
			enc := json.NewEncoder(os.Stdout)
			_ = enc.Encode(msg)
		} else {
			fmt.Printf("brain/index: %d info + %d facts would be indexed (%d streamed, %d duplicate skipped) (%v)\n",
				stats.Total, len(facts), stats.Streamed, stats.Streamed-stats.Total, stats.BySource)
		}
		return 0
	}
	if !v.rebuild && !v.skip {
		fmt.Fprintln(os.Stderr, "brain/index: refuse write; pass --rebuild (fresh db) or --skip (resume existing db)")
		return 2
	}

	if !v.skip {
		// A live holder of this file (e.g. brain-serve in a compose container
		// bind-mounting the same var/) would keep serving the removed inode:
		// reads go stale and the fresh db lands with the writer's uid, locking
		// out the service. Refuse unless --force. Two probes: same-namespace
		// fd holders via /proc, and (for the default repo db) any brain API
		// answering on 127.0.0.1:$KB_PORT — container fds are invisible to
		// host /proc when the service runs as another uid.
		var reasons []string
		if holders, err := brain.LiveHolders(dbpath); err != nil {
			fmt.Fprintf(os.Stderr, "brain/index: live-holder check: %v\n", err)
			return 1
		} else if len(holders) > 0 {
			reasons = append(reasons, fmt.Sprintf("%s is open by %d process(es):\n  %s", dbpath, len(holders), strings.Join(holders, "\n  ")))
		}
		if gitRoot := gitRepoRoot(); gitRoot != "" &&
			dbpath == filepath.Join(gitRoot, "var", "kb.lbug") &&
			!cfg.IndexAllowLive &&
			brain.BrainAPIAlive("127.0.0.1:"+port) {
			reasons = append(reasons, fmt.Sprintf("a brain API is answering on 127.0.0.1:%s (compose brain bind-mounts this db)", port))
		}
		if len(reasons) > 0 && !v.force {
			fmt.Fprintf(os.Stderr, "brain/index: refuse --rebuild; stop/restart the brain first (or pass --force):\n%s\n", strings.Join(reasons, "\n"))
			return 2
		}
		_ = os.Remove(dbpath)
		_ = os.Remove(dbpath + ".wal")
	}

	// B3 (issue #248): embedding-колонка в БД пишется ТОЛЬКО когда ANN выключен
	// (тогда векторный путь — linear-scan fallback по l.embedding). Когда ANN
	// включён (vector.ann.enabled=true) — эмбеддинги в БД не пишутся вовсе:
	// модель не грузится (минус ~1.5GB RSS write-фазы), WriteCorpus получает
	// model=nil (leafs пишутся текстом без колонки), facts тоже без эмбеддинга.
	// ANN строит отдельный шаг волны ann-build (bin/brain/ann.go ensure) из
	// текста напрямую (см. anntool.extractRows) — колонка ему не нужна.
	var model *brain.StaticModel
	if !cfg.Vector.ANN.Enabled {
		model, err = brain.LoadModel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "brain/index: model: %v\n", err)
			return 1
		}
		defer model.Close()
	}

	db, conn, err := brain.OpenWritable(dbpath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain/index: %v\n", err)
		return 1
	}
	defer db.Close()
	defer conn.Close()
	if err := brain.InitSchema(conn); err != nil {
		fmt.Fprintf(os.Stderr, "brain/index: schema: %v\n", err)
		return 1
	}

	prog := brain.NewProgressReporter(os.Stderr, time.Duration(v.progress)*time.Second)
	// Resume: existing-set собирается ОДИН раз на старте (до чанков) и
	// передаётся в WriteCorpus через WriteOptions.Existing (issue #237).
	var existing map[string]bool
	if v.skip {
		if existing, err = brain.ExistingLeafIDSet(conn); err != nil {
			fmt.Fprintf(os.Stderr, "brain/index: resume set: %v\n", err)
			return 1
		}
	}
	// Pass 2: стрим корпуса → чанки по --chunk → WriteCorpus(чанк) → буфер
	// освобождается. Пиковая память ограничена чанком (~2048 leafs + их
	// эмбеддинги), весь корпус в памяти не держится.
	infoN, err := brain.WriteCorpusChunked(ctx, sources(v, root), v.chunk, v.limit, stats, func(chunk []contract.Leaf, base, total int) (int, error) {
		return brain.WriteCorpus(conn, chunk, model, brain.WriteOptions{
			Workers: v.workers, Batch: v.batch, Skip: v.skip, Existing: existing,
			Progress: prog, ProgressDone: base, ProgressTotal: total,
		})
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain/index: write info: %v\n", err)
		return 1
	}
	factN := 0
	for _, f := range facts {
		if f.Text == "" || f.Source == "" {
			continue
		}
		if len(f.Embedding) == 0 && model != nil {
			vec, err := model.Embed(f.Text)
			if err != nil {
				fmt.Fprintf(os.Stderr, "brain/index: embed fact: %v\n", err)
				return 1
			}
			f.Embedding = vec
		}
		if f.Root == "" {
			f.Root = "facts"
		}
		if f.Type == "" {
			f.Type = "fact"
		}
		if f.How == "" {
			f.How = "facts/extract"
		}
		if _, err := brain.UpsertLeaf(conn, f); err != nil {
			fmt.Fprintf(os.Stderr, "brain/index: fact: %v\n", err)
			return 1
		}
		factN++
	}
	if !v.skipIndexes {
		// Фаза индексов (issue #244): CREATE_FTS_INDEX на полном корпусе
		// падает на дефолтном пуле 1GB (buffer pool full) и оставляет
		// орфан-таблицу 0_id_appears_info, недостижимую через SQL. Пул для
		// этой фазы подбирается по размеру текста корпуса; write-хэндл
		// закрывается, БД переоткрывается с нужным пулом (автоматизация
		// двухфазного workaround-а из docs/brain/rebuild.md).
		totalChars, err := brain.CorpusTextChars(conn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "brain/index: corpus chars: %v\n", err)
			return 1
		}
		conn.Close()
		db.Close()
		if err := brain.BuildIndexes(dbpath, totalChars); err != nil {
			fmt.Fprintf(os.Stderr, "brain/index: indexes: %v\n", err)
			return 1
		}
	}
	total := infoN + factN
	if v.jsonOut {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"indexed_info": infoN, "indexed_facts": factN, "db": dbpath, "total": total,
			"by_source": stats.BySource, "streamed": stats.Streamed,
		})
	} else {
		fmt.Printf("indexed %d/%d info (streamed %d, dedup %d) + %d facts; db total %d\n",
			infoN, stats.Total, stats.Streamed, stats.Streamed-stats.Total, factN, total)
	}
	return 0
}

func filterCorpusArgs(args []string, corpus *[]string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--corpus" && i+1 < len(args):
			*corpus = append(*corpus, args[i+1])
			i++
		case strings.HasPrefix(a, "--corpus="):
			*corpus = append(*corpus, strings.TrimPrefix(a, "--corpus="))
		default:
			out = append(out, a)
		}
	}
	return out
}

func hasBareWithChats(args []string) bool {
	for i, a := range args {
		if a == "--with-chats" {
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return true
			}
		}
	}
	return false
}

func factsFromExtract(root string) []brain.LeafInput {
	cmd := exec.Command(filepath.Join(root, "bin", "cgo", "zig"), "go", "run",
		"-tags=system_ladybug,facts_extract",
		filepath.Join(root, "bin", "facts", "extract.go"), "--json", "--dry-run")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain/index: facts/extract failed: %v\n", err)
		return nil
	}
	return factsFromJSON(out)
}

func factsFromJSON(raw []byte) []brain.LeafInput {
	var payload any
	if json.Unmarshal(raw, &payload) != nil {
		return nil
	}
	var list []any
	switch t := payload.(type) {
	case map[string]any:
		list, _ = t["facts"].([]any)
	case []any:
		list = t
	}
	var out []brain.LeafInput
	for _, x := range list {
		m, ok := x.(map[string]any)
		if !ok {
			continue
		}
		text := fmt.Sprint(m["text"])
		source := fmt.Sprint(m["source"])
		if text == "" || source == "" || text == "<nil>" || source == "<nil>" {
			continue
		}
		out = append(out, brain.LeafInput{
			Text: text, Source: source, Root: "facts", Confidence: "confirmed",
			SourceRev:  strOr(m["source_rev"], "working-tree"),
			How:        strOr(m["how"], "facts/extract"),
			Loc:        strOr(m["loc"], source),
			Type:       "fact",
			ExternalID: strOr(m["external_id"], ""),
			ObservedAt: strOr(m["observed_at"], ""),
		})
	}
	return out
}

func strOr(v any, def string) string {
	if v == nil {
		return def
	}
	s := fmt.Sprint(v)
	if s == "" || s == "<nil>" {
		return def
	}
	return s
}
