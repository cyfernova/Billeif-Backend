package spreadsheet

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestXLSXUsesTypedCellsBusinessTimezoneAndFormulaSafety(t *testing.T) {
	location, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatal(err)
	}
	workbook, err := XLSX(
		[]Column{{Label: "Party", Type: "string"}, {Label: "Amount", Type: "number"}, {Label: "Occurred", Type: "datetime"}, {Label: "Calendar Date", Type: "date"}},
		[][]any{{" =HYPERLINK(\"https://attacker.example\")", "125.50", time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC), time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}},
		location,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(workbook, []byte("PK")) {
		t.Fatalf("workbook is not a ZIP/XLSX file: %x", workbook[:min(4, len(workbook))])
	}
	if utf8.Valid(workbook) {
		t.Fatal("workbook must be detected as binary by the Lambda HTTP adapter")
	}

	sheet := readZIPPart(t, workbook, "xl/worksheets/sheet1.xml")
	for _, want := range []string{
		`<c r="A2" t="inlineStr"><is><t xml:space="preserve">&apos; =HYPERLINK(&quot;https://attacker.example&quot;)</t></is></c>`,
		`<c r="B2" s="2"><v>125.50</v></c>`,
		`<c r="C2" s="5"><v>46267.0625</v></c>`, // 2026-09-02 01:30 Asia/Kolkata
		`<c r="D2" s="4"><v>46266</v></c>`,
	} {
		if !strings.Contains(sheet, want) {
			t.Fatalf("sheet missing %q:\n%s", want, sheet)
		}
	}
}

func TestXLSXRejectsUnsafeBounds(t *testing.T) {
	rows := make([][]any, MaxXLSXRows+1)
	for index := range rows {
		rows[index] = []any{"safe"}
	}
	if _, err := XLSX([]Column{{Label: "Value", Type: "string"}}, rows, time.UTC); !errors.Is(err, ErrXLSXBoundsExceeded) {
		t.Fatalf("expected bounds error, got %v", err)
	}
	if _, err := XLSX([]Column{{Label: "Value", Type: "string"}}, [][]any{{strings.Repeat("x", MaxXLSXCellCharacters+1)}}, time.UTC); !errors.Is(err, ErrXLSXBoundsExceeded) {
		t.Fatalf("expected cell bounds error, got %v", err)
	}
}

func readZIPPart(t *testing.T, workbook []byte, name string) string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(workbook), int64(len(workbook)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range reader.File {
		if file.Name != name {
			continue
		}
		part, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer part.Close()
		data, err := io.ReadAll(part)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	t.Fatalf("ZIP part %s not found", name)
	return ""
}
