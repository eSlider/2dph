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

// WALEncoder appends rows to a WAL writer.
type WALEncoder struct {
	w *bufio.Writer
}

func newWALEncoder(f *os.File) *WALEncoder {
	return &WALEncoder{w: bufio.NewWriter(f)}
}

// Append writes one row as a JSON line and flushes (durable enough between
// waves; the snapshot is the recovery point).
func (e *WALEncoder) Append(r Row) error {
	line, err := json.Marshal(r)
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

func (d *WALDecoder) Next() (Row, error) {
	if !d.sc.Scan() {
		return Row{}, io.EOF
	}
	var r Row
	if err := json.Unmarshal(d.sc.Bytes(), &r); err != nil {
		return Row{}, io.EOF // torn line: treat as end of valid prefix
	}
	return r, nil
}
