package ann

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
)

// The append-only WAL stores upserted rows since the last snapshot, one JSON
// object per line. JSONL (not gob) so a torn tail from a crash mid-append
// never poisons the replay: the decoder stops at the first bad line and the
// valid prefix sticks.
//
// A line is either an upserted row ({"ID":..,"Vec":[..]}) or, since #4, a
// tombstone written by Delete ({"ID":..,"del":true}). The embedded Row keeps
// the legacy shape, so a WAL written before tombstones replays unchanged.

// WALRecord is one WAL line: a row, or a delete marker when Del is set.
type WALRecord struct {
	Row
	Del bool `json:"del,omitempty"`
}

// WALEncoder appends rows to a WAL writer.
type WALEncoder struct {
	w *bufio.Writer
}

func newWALEncoder(f *os.File) *WALEncoder {
	return &WALEncoder{w: bufio.NewWriter(f)}
}

// Append writes one row as a JSON line and flushes (durable enough between
// waves; the snapshot is the recovery point).
func (e *WALEncoder) Append(r Row) error { return e.write(WALRecord{Row: r}) }

// AppendDel writes a tombstone for id, so a restart between Delete and the
// next Save keeps the id dead (a compaction would drop it anyway).
func (e *WALEncoder) AppendDel(id string) error {
	return e.write(WALRecord{Row: Row{ID: id}, Del: true})
}

func (e *WALEncoder) write(rec WALRecord) error {
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if _, err := e.w.Write(append(line, '\n')); err != nil {
		return err
	}
	return e.w.Flush()
}

// WALDecoder reads rows from a WAL stream; Next returns io.EOF at a clean
// end and nil for a torn tail (callers keep the valid prefix).
type WALDecoder struct {
	sc *bufio.Scanner
}

func newWALDecoder(r io.Reader) *WALDecoder {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	return &WALDecoder{sc: sc}
}

func (d *WALDecoder) Next() (WALRecord, error) {
	if !d.sc.Scan() {
		return WALRecord{}, io.EOF
	}
	var r WALRecord
	if err := json.Unmarshal(d.sc.Bytes(), &r); err != nil {
		return WALRecord{}, io.EOF // torn line: treat as end of valid prefix
	}
	return r, nil
}
