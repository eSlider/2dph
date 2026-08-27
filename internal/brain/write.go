//go:build cgo && system_ladybug

package brain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	lbug "github.com/LadybugDB/go-ladybug"

	"github.com/eSlider/2dph/internal/facts"
)

const EmbedDim = 256

// LeafInput is one facts|info leaf for AddLeafs / upsert.
type LeafInput struct {
	Text       string
	Root       string
	Confidence string
	Source     string
	SourceRev  string
	How        string
	Loc        string
	Type       string
	ValidFrom  string
	ValidTo    string
	Embedding  []float64
}

// OpenWritable opens kb.lbug for writes (ReadOnly=false) and loads FTS+VECTOR.
func OpenWritable(path string) (*lbug.Database, *lbug.Connection, error) {
	if path == "" {
		path = dbPath()
	}
	if err := os.MkdirAll(filepathDir(path), 0o755); err != nil {
		return nil, nil, err
	}
	cfg := lbug.DefaultSystemConfig()
	cfg.ReadOnly = false
	cfg.MaxNumThreads = 8
	cfg.BufferPoolSize = 1 << 30
	if v := brainCfg().BufferPool; v > 0 {
		cfg.BufferPoolSize = uint64(v)
	}
	db, err := lbug.OpenDatabase(path, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("OpenDatabase: %w", err)
	}
	conn, err := lbug.OpenConnection(db)
	if err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("OpenConnection: %w", err)
	}
	if err := loadExt(conn, "FTS"); err != nil {
		conn.Close()
		db.Close()
		return nil, nil, err
	}
	if err := loadExt(conn, "VECTOR"); err != nil {
		conn.Close()
		db.Close()
		return nil, nil, err
	}
	return db, conn, nil
}

func filepathDir(path string) string {
	i := strings.LastIndexAny(path, `/\`)
	if i < 0 {
		return "."
	}
	return path[:i]
}

func loadExt(conn *lbug.Connection, name string) error {
	res, _ := conn.Query("INSTALL " + name)
	qClose(res)
	if res, err := conn.Query("LOAD EXTENSION " + name); err != nil {
		qClose(res)
		return fmt.Errorf("LOAD EXTENSION %s: %w", name, err)
	} else {
		qClose(res)
	}
	return nil
}

// InitSchema creates Leaf/File/Commit/Person tables (idempotent).
func InitSchema(conn *lbug.Connection) error {
	stmts := []string{
		`CREATE NODE TABLE IF NOT EXISTS Leaf (
 id STRING, text STRING, root STRING, confidence STRING,
 sha256 STRING, source STRING, source_rev STRING, observed_at STRING,
 how STRING, loc STRING, type STRING,
 valid_from STRING, valid_to STRING,
 embedding FLOAT[256],
 PRIMARY KEY(id))`,
		`CREATE NODE TABLE IF NOT EXISTS File (
 id STRING, path STRING, repo STRING, mtime STRING, PRIMARY KEY(id))`,
		`CREATE REL TABLE IF NOT EXISTS FROM_FILE (FROM Leaf TO File)`,
		`CREATE NODE TABLE IF NOT EXISTS Host (id STRING, hostname STRING, user STRING, PRIMARY KEY(id))`,
		`CREATE REL TABLE IF NOT EXISTS RUNS_ON (FROM Leaf TO Host)`,
		`CREATE NODE TABLE IF NOT EXISTS Commit (id STRING, repo STRING, subject STRING,
author STRING, email STRING, date STRING, PRIMARY KEY(id))`,
		`CREATE NODE TABLE IF NOT EXISTS Person (id STRING, name STRING, email STRING, PRIMARY KEY(id))`,
		`CREATE REL TABLE IF NOT EXISTS AUTHORED (FROM Commit TO Person)`,
		// SYNAPTIC models user-defined edges between neurons (leafs): issue #82
		// "Synapse Matrix". `type` is the synapse label (default "synapse").
		`CREATE REL TABLE IF NOT EXISTS SYNAPTIC (FROM Leaf TO Leaf, type STRING)`,
	}
	for _, s := range stmts {
		if res, err := conn.Query(s); err != nil {
			qClose(res)
			return err
		} else {
			qClose(res)
		}
	}
	for _, col := range []string{"valid_from", "valid_to"} {
		res, _ := conn.Query("ALTER TABLE Leaf ADD " + col + " STRING")
		qClose(res)
	}
	return nil
}

// textSHA hashes text for the leaf.sha256 property.
func textSHA(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func execParams(conn *lbug.Connection, query string, args map[string]any) error {
	ps, err := conn.Prepare(query)
	if err != nil {
		return err
	}
	defer ps.Close()
	res, err := conn.Execute(ps, args)
	if err != nil {
		return err
	}
	res.Close()
	return nil
}

// UpsertLeaf MERGEs one leaf. Safe while FTS/HNSW exist (no DROP INDEX).
func UpsertLeaf(conn *lbug.Connection, lf LeafInput) (string, error) {
	lf.Text = strings.ToValidUTF8(lf.Text, "\uFFFD")
	lf.Source = strings.ToValidUTF8(lf.Source, "\uFFFD")
	if lf.Text == "" || lf.Source == "" {
		return "", fmt.Errorf("leaf needs text and source")
	}
	if lf.Root == "" {
		lf.Root = "info"
	}
	if lf.Confidence == "" {
		lf.Confidence = "confirmed"
	}
	if lf.SourceRev == "" {
		lf.SourceRev = "working-tree"
	}
	if lf.How == "" {
		lf.How = "brain/add"
	}
	if lf.Loc == "" {
		lf.Loc = lf.Source
	}
	if lf.Type == "" {
		lf.Type = "reference"
	}
	lid := LeafID(lf.Text, lf.Source)
	obs := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	vf := facts.NormalizeDay(lf.ValidFrom)
	vt := facts.NormalizeDay(lf.ValidTo)

	q := `MERGE (l:Leaf {id:$id})
SET l.text=$text, l.root=$root, l.confidence=$confidence,
    l.sha256=$sha, l.source=$source, l.source_rev=$rev, l.observed_at=$obs,
    l.how=$how, l.loc=$location, l.type=$type,
    l.valid_from=$vf, l.valid_to=$vt`
	args := map[string]any{
		"id": lid, "text": lf.Text, "root": lf.Root, "confidence": lf.Confidence,
		"sha": textSHA(lf.Text), "source": lf.Source, "rev": lf.SourceRev,
		"obs": obs, "how": lf.How, "location": lf.Loc, "type": lf.Type,
		"vf": vf, "vt": vt,
	}
	if len(lf.Embedding) > 0 {
		emb := padEmbed(lf.Embedding)
		q += `, l.embedding=$emb`
		args["emb"] = emb
	}
	if err := execParams(conn, q, args); err != nil {
		return "", err
	}
	return lid, nil
}

func padEmbed(v []float64) []any {
	out := make([]any, EmbedDim)
	for i := 0; i < EmbedDim; i++ {
		if i < len(v) {
			out[i] = v[i]
		} else {
			out[i] = 0.0
		}
	}
	return out
}

// AddLeafs writes leafs in one transaction when BEGIN is available.
func AddLeafs(conn *lbug.Connection, leafs []LeafInput) ([]string, error) {
	if len(leafs) == 0 {
		return nil, nil
	}
	started := false
	if res, err := conn.Query("BEGIN TRANSACTION"); err == nil {
		qClose(res)
		started = true
	} else {
		qClose(res)
	}
	ids := make([]string, 0, len(leafs))
	for _, lf := range leafs {
		id, err := UpsertLeaf(conn, lf)
		if err != nil {
			if started {
				res, _ := conn.Query("ROLLBACK")
				qClose(res)
			}
			return nil, err
		}
		ids = append(ids, id)
	}
	if started {
		if res, err := conn.Query("COMMIT"); err != nil {
			qClose(res)
			return nil, err
		} else {
			qClose(res)
		}
	}
	return ids, nil
}

// FileID matches kblib: repo:path or path.
func FileID(repo, path string) string {
	if repo != "" {
		return repo + ":" + path
	}
	return path
}

// LinkFromFile MERGEs File and Leaf-[:FROM_FILE]->File.
func LinkFromFile(conn *lbug.Connection, leafID, path, repo, mtime string) (string, error) {
	fid := FileID(repo, path)
	if err := execParams(conn,
		`MERGE (f:File {id:$id}) SET f.path=$path, f.repo=$repo, f.mtime=$mtime`,
		map[string]any{"id": fid, "path": path, "repo": repo, "mtime": mtime},
	); err != nil {
		return "", err
	}
	if err := execParams(conn,
		`MATCH (l:Leaf {id:$lid}), (f:File {id:$fid}) MERGE (l)-[:FROM_FILE]->(f)`,
		map[string]any{"lid": leafID, "fid": fid},
	); err != nil {
		return "", err
	}
	return fid, nil
}

func leafIndexNames(conn *lbug.Connection) (map[string]bool, error) {
	res, err := conn.Query("CALL SHOW_INDEXES() RETURN *")
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
		// SHOW_INDEXES columns: table, name, ...
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 2 {
			continue
		}
		table, _ := vals[0].(string)
		name, _ := vals[1].(string)
		if table == "Leaf" && name != "" {
			out[name] = true
		}
	}
	return out, nil
}

// EnsureIndexes creates the FTS index when missing. Never DROP INDEX.
//
// The HNSW vector index (Leaf_vec) is deliberately NOT created here: liblbug
// 0.19.0 SIGSEGVs on HNSW insert once the graph passes ~1300 nodes (NULL
// deref in simsimd_cos_f32 during OnDiskHNSWIndex::shrinkForNode). Vector
// ranking now runs in-process via queryVector's brute-force cosine scan
// (search.go), so Leaf_vec is neither required nor used. An existing index is
// left in place (untouched) so data is never dropped.
func EnsureIndexes(conn *lbug.Connection) error {
	names, err := leafIndexNames(conn)
	if err != nil {
		return err
	}
	if !names["id"] {
		if res, err := conn.Query("CALL CREATE_FTS_INDEX('Leaf', 'id', ['text'])"); err != nil {
			qClose(res)
			return fmt.Errorf("CREATE_FTS_INDEX: %w (delete kb.lbug and --rebuild)", err)
		} else {
			qClose(res)
		}
	}
	names, err = leafIndexNames(conn)
	if err != nil {
		return err
	}
	for _, need := range []string{"id"} {
		if !names[need] {
			return fmt.Errorf("Leaf indexes incomplete: missing %s; have %v", need, names)
		}
	}
	return nil
}
