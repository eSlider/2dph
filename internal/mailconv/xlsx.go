package mailconv

import (
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

// xlsxHandler renders an OOXML spreadsheet (.xlsx) as markdown. Spreadsheets
// used to be metadata-only (#308), but the accounting workbooks that arrive by
// mail (Kontoblatt, Übersicht, Offene Posten, monthly Auswertungen) only make
// sense when their cells are searchable, so every sheet becomes a table.
//
// The header row and column count are deduced: bookkeeping sheets often carry
// a title/logo row above the real header and trailing empty columns. We look
// for the row with the most known column titles within the first rows; if none
// is recognised we fall back to the first multi-cell, mostly non-numeric row.
func xlsxHandler(path, name, _ string, _ bool) (string, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	sheets := f.GetSheetList()
	var b strings.Builder
	fmt.Fprintf(&b, "**%s** — sheets: %s\n", name, strings.Join(sheets, ", "))
	for i, sh := range sheets {
		if i >= xlsxMaxSheets {
			fmt.Fprintf(&b, "\n_(%d more sheets omitted)_\n", len(sheets)-xlsxMaxSheets)
			break
		}
		if b.Len() >= xlsxMaxBytes {
			b.WriteString("\n_(output truncated)_\n")
			break
		}
		rows, err := f.GetRows(sh)
		if err != nil {
			fmt.Fprintf(&b, "\n## %s\n\n<!-- %s: %v -->\n", sh, name, err)
			continue
		}
		renderSheet(&b, sh, rows)
	}
	return strings.TrimRight(b.String(), "\n") + "\n", nil
}

const (
	xlsxHeaderScan = 15
	xlsxMaxRows    = 2000
	xlsxMaxSheets  = 40
	xlsxMaxCols    = 40
	xlsxMaxCell    = 200
	xlsxMaxBytes   = 400 * 1024
)

// xlsxHeaderWords are column titles seen in the accounting workbooks. They are
// used only to score a candidate header row, never to drop a sheet.
var xlsxHeaderWords = []string{
	"datum", "gegenkonto", "buchungstext", "buchung", "beleg", "belegnr",
	"umsatz", "soll", "haben", "rechnungsnummer", "rechnung", "betrag",
	"summe", "gesamt", "saldo", "stand", "konto", "kontonr", "kontonummer",
	"name", "vorname", "nachname", "monat", "jahr", "beschreibung",
	"bezeichnung", "nummer", "nr", "anzahl", "tatbestand", "text", "wert",
	"währung", "ust", "mwst", "netto", "brutto", "offen", "forderung",
	"mahnstufe", "debitor", "kreditor", "status", "prüf", "pruef",
	"anmerkung", "bemerkung", "sachkonto", "kostenstelle", "woche",
	"stunden", "leistung", "patient", "klient", "vertrag", "versicherung",
	"art", "typ", "hinweis", "ausgabe", "einnahme", "zahlung", "eingang",
}

func renderSheet(b *strings.Builder, name string, rows [][]string) {
	rows = trimRows(rows)
	if len(rows) == 0 {
		fmt.Fprintf(b, "\n## %s\n\n_(empty)_\n", name)
		return
	}
	hdr := deduceHeader(rows)
	width := deduceWidth(rows, hdr)
	fmt.Fprintf(b, "\n## %s\n\n", name)
	start := 0
	if hdr >= 0 {
		writeTableRow(b, rows[hdr], width)
		writeSeparator(b, width)
		start = hdr + 1
	}
	n := 0
	for i := start; i < len(rows); i++ {
		if isEmptyRow(rows[i]) {
			continue
		}
		writeTableRow(b, rows[i], width)
		n++
		if n >= xlsxMaxRows {
			fmt.Fprintf(b, "\n_(%d more rows omitted)_\n", len(rows)-i-1)
			break
		}
	}
	b.WriteString("\n")
}

// deduceHeader returns the index of the most likely header row, or -1 when the
// sheet looks header-less (raw export without titles).
func deduceHeader(rows [][]string) int {
	limit := min(len(rows), xlsxHeaderScan)
	best, bestScore := -1, 0
	for i := 0; i < limit; i++ {
		score, nonEmpty := 0, 0
		for _, c := range rows[i] {
			c = strings.ToLower(strings.TrimSpace(c))
			if c == "" {
				continue
			}
			nonEmpty++
			if matchesHeaderWord(c) {
				score++
			}
		}
		if nonEmpty >= 2 && score > bestScore {
			best, bestScore = i, score
		}
	}
	if bestScore >= 2 {
		return best
	}
	for i := 0; i < limit; i++ {
		nonEmpty, numeric := 0, 0
		for _, c := range rows[i] {
			c = strings.TrimSpace(c)
			if c == "" {
				continue
			}
			nonEmpty++
			if looksNumeric(c) {
				numeric++
			}
		}
		if nonEmpty >= 2 && numeric*2 < nonEmpty {
			return i
		}
	}
	return -1
}

func matchesHeaderWord(cell string) bool {
	for _, w := range xlsxHeaderWords {
		if cell == w || strings.Contains(cell, w) {
			return true
		}
	}
	return false
}

// deduceWidth is the widest data row around the header, capped so a single
// stray far-right cell does not stretch the table.
func deduceWidth(rows [][]string, hdr int) int {
	start := 0
	if hdr > 0 {
		start = hdr
	}
	end := min(len(rows), start+100)
	width := 0
	for i := start; i < end; i++ {
		if n := nonEmptyCells(rows[i]); n > width {
			width = n
		}
	}
	if width > xlsxMaxCols {
		width = xlsxMaxCols
	}
	if width == 0 {
		width = 1
	}
	return width
}

func writeTableRow(b *strings.Builder, row []string, width int) {
	b.WriteString("|")
	for i := 0; i < width; i++ {
		cell := ""
		if i < len(row) {
			cell = row[i]
		}
		fmt.Fprintf(b, " %s |", sanitizeCell(cell))
	}
	b.WriteString("\n")
}

func writeSeparator(b *strings.Builder, width int) {
	b.WriteString("|")
	for i := 0; i < width; i++ {
		b.WriteString(" --- |")
	}
	b.WriteString("\n")
}

// sanitizeCell keeps one markdown table cell: no newlines, escaped pipes,
// bounded length.
func sanitizeCell(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", "\\|")
	if len(s) > xlsxMaxCell {
		s = s[:xlsxMaxCell] + "…"
	}
	return s
}

// trimRows drops trailing empty rows and trailing empty cells.
func trimRows(rows [][]string) [][]string {
	for len(rows) > 0 && isEmptyRow(rows[len(rows)-1]) {
		rows = rows[:len(rows)-1]
	}
	for i, r := range rows {
		for len(r) > 0 && strings.TrimSpace(r[len(r)-1]) == "" {
			r = r[:len(r)-1]
		}
		rows[i] = r
	}
	return rows
}

func isEmptyRow(row []string) bool { return nonEmptyCells(row) == 0 }

func nonEmptyCells(row []string) int {
	n := 0
	for _, c := range row {
		if strings.TrimSpace(c) != "" {
			n++
		}
	}
	return n
}

// looksNumeric reports whether a cell is a bare number/amount/date rather than
// a column title, so the header fallback can skip data rows.
func looksNumeric(s string) bool {
	digits := 0
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case strings.ContainsRune(".,-+/: €$%", r):
		default:
			return false
		}
	}
	return digits > 0
}
