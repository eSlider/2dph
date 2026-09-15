package mailconv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

const xlsxMIME = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

func writeXLSX(t *testing.T, build func(f *excelize.File) error) string {
	t.Helper()
	f := excelize.NewFile()
	if err := build(f); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "book.xlsx")
	if err := f.SaveAs(p); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

// The real workbooks carry a title row above the header; the handler must find
// the Datum/Gegenkonto/… row and render it as the table header.
func TestXLSXHandlerDeducesHeaderRow(t *testing.T) {
	p := writeXLSX(t, func(f *excelize.File) error {
		sh := f.GetSheetName(0)
		_ = f.SetCellValue(sh, "A1", "Edelweiss - Kontoblatt 1591")
		_ = f.SetCellValue(sh, "A3", "Datum")
		_ = f.SetCellValue(sh, "B3", "Gegenkonto")
		_ = f.SetCellValue(sh, "C3", "Buchungstext")
		_ = f.SetCellValue(sh, "D3", "Umsatz Soll")
		_ = f.SetCellValue(sh, "E3", "Umsatz Haben")
		_ = f.SetCellValue(sh, "A4", "02.01.2025")
		_ = f.SetCellValue(sh, "B4", 71900)
		_ = f.SetCellValue(sh, "C4", "Office Discount, RE fehlt")
		_ = f.SetCellValue(sh, "D4", 44.14)
		return nil
	})
	got := ConvertAttachment(p, "Kontoblatt.xlsx", xlsxMIME, false)
	for _, want := range []string{
		"Kontoblatt.xlsx", "Datum", "Gegenkonto", "Umsatz Soll",
		"Office Discount, RE fehlt", "| 71900 |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("xlsx output missing %q:\n%s", want, got)
		}
	}
}

// Summary sheets (Übersicht/Summen) are ordinary sheets and must render too.
func TestXLSXHandlerSummarySheet(t *testing.T) {
	p := writeXLSX(t, func(f *excelize.File) error {
		if _, err := f.NewSheet("Übersicht EDW"); err != nil {
			return err
		}
		sh := "Übersicht EDW"
		_ = f.SetCellValue(sh, "A1", "Position")
		_ = f.SetCellValue(sh, "B1", "Betrag")
		_ = f.SetCellValue(sh, "A2", "Summe")
		_ = f.SetCellValue(sh, "B2", 1234.5)
		return nil
	})
	got := ConvertAttachment(p, "EDW.xlsx", xlsxMIME, false)
	for _, want := range []string{"## Übersicht EDW", "Position", "Summe"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary output missing %q:\n%s", want, got)
		}
	}
}

// A file that is not a spreadsheet must degrade to a comment, never abort.
func TestXLSXHandlerBadFileIsComment(t *testing.T) {
	p := filepath.Join(t.TempDir(), "broken.xlsx")
	if err := os.WriteFile(p, []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ConvertAttachment(p, "broken.xlsx", xlsxMIME, false)
	if !strings.Contains(got, "<!-- broken.xlsx:") {
		t.Errorf("bad xlsx = %q", got)
	}
}

func TestLooksNumeric(t *testing.T) {
	cases := map[string]bool{
		"1.234,56": true, "44.14": true, "02.01.2025": true, "71900": true,
		"€ 12,00": true, "": false, "Datum": false, "Summe": false,
		"Office Discount": false,
	}
	for in, want := range cases {
		if got := looksNumeric(in); got != want {
			t.Errorf("looksNumeric(%q) = %v, want %v", in, got, want)
		}
	}
}
