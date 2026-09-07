package spreadsheet

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	MaxXLSXRows              = 500
	MaxXLSXColumns           = 64
	MaxXLSXCellCharacters    = 32767
	MaxXLSXUncompressedBytes = 8 << 20
)

var ErrXLSXBoundsExceeded = errors.New("spreadsheet export exceeds safe bounds")

type Column struct {
	Label string
	Type  string
}

// XLSX creates a single-sheet workbook with bounded memory use. Number and
// date columns use native spreadsheet cells; strings are formula-neutralized.
func XLSX(columns []Column, rows [][]any, location *time.Location) ([]byte, error) {
	if location == nil {
		location = time.UTC
	}
	if len(columns) == 0 || len(columns) > MaxXLSXColumns || len(rows) > MaxXLSXRows {
		return nil, ErrXLSXBoundsExceeded
	}
	for _, row := range rows {
		if len(row) != len(columns) {
			return nil, fmt.Errorf("spreadsheet row width does not match columns")
		}
	}

	worksheet, err := worksheetXML(columns, rows, location)
	if err != nil {
		return nil, err
	}
	parts := map[string]string{
		"[Content_Types].xml":        `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/></Types>`,
		"_rels/.rels":                `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`,
		"xl/workbook.xml":            `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Report" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`,
		"xl/styles.xml":              stylesXML,
		"xl/worksheets/sheet1.xml":   worksheet,
	}

	var output bytes.Buffer
	zw := zip.NewWriter(&output)
	// The Lambda HTTP adapter detects binary responses from UTF-8 validity.
	// A non-UTF-8 ZIP comment makes API Gateway base64 behavior deterministic.
	if err := zw.SetComment(string([]byte{0xff})); err != nil {
		return nil, err
	}
	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "xl/workbook.xml", "xl/_rels/workbook.xml.rels", "xl/styles.xml", "xl/worksheets/sheet1.xml"} {
		entry, createErr := zw.Create(name)
		if createErr != nil {
			return nil, createErr
		}
		if _, writeErr := entry.Write([]byte(parts[name])); writeErr != nil {
			return nil, writeErr
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

const stylesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><numFmts count="3"><numFmt numFmtId="164" formatCode="#,##0.00"/><numFmt numFmtId="165" formatCode="yyyy-mm-dd"/><numFmt numFmtId="166" formatCode="yyyy-mm-dd hh:mm:ss"/></numFmts><fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts><fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills><borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="6"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/><xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/><xf numFmtId="1" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/><xf numFmtId="165" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/><xf numFmtId="166" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/></cellXfs><cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`

func worksheetXML(columns []Column, rows [][]any, location *time.Location) (string, error) {
	var out strings.Builder
	out.Grow(min(MaxXLSXUncompressedBytes, (len(rows)+1)*len(columns)*64))
	out.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews><sheetData>`)

	out.WriteString(`<row r="1">`)
	for index, column := range columns {
		if err := writeInlineCell(&out, cellReference(index, 1), column.Label, 1); err != nil {
			return "", err
		}
	}
	out.WriteString(`</row>`)

	for rowIndex, row := range rows {
		out.WriteString(`<row r="`)
		out.WriteString(strconv.Itoa(rowIndex + 2))
		out.WriteString(`">`)
		for columnIndex, value := range row {
			if err := writeTypedCell(&out, cellReference(columnIndex, rowIndex+2), columns[columnIndex].Type, value, location); err != nil {
				return "", err
			}
			if out.Len() > MaxXLSXUncompressedBytes {
				return "", ErrXLSXBoundsExceeded
			}
		}
		out.WriteString(`</row>`)
	}
	out.WriteString(`</sheetData></worksheet>`)
	return out.String(), nil
}

func writeTypedCell(out *strings.Builder, ref, columnType string, value any, location *time.Location) error {
	if value == nil {
		out.WriteString(`<c r="` + ref + `"/>`)
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(columnType)) {
	case "number":
		if number, ok := spreadsheetNumber(value); ok {
			writeNumberTextCell(out, ref, number, 2)
			return nil
		}
	case "integer":
		if number, ok := spreadsheetNumber(value); ok {
			writeNumberTextCell(out, ref, number, 3)
			return nil
		}
	case "date":
		if date, ok := spreadsheetDate(value); ok {
			writeNumberCell(out, ref, excelSerial(date), 4)
			return nil
		}
	case "datetime":
		if date, _, ok := spreadsheetTime(value, location); ok {
			writeNumberCell(out, ref, excelSerial(date), 5)
			return nil
		}
	case "boolean", "bool":
		if value, ok := value.(bool); ok {
			out.WriteString(`<c r="` + ref + `" t="b"><v>`)
			if value {
				out.WriteByte('1')
			} else {
				out.WriteByte('0')
			}
			out.WriteString(`</v></c>`)
			return nil
		}
	}
	return writeInlineCell(out, ref, fmt.Sprint(value), 0)
}

func writeInlineCell(out *strings.Builder, ref, value string, style int) error {
	value = SafeCell(value).(string)
	if len([]rune(value)) > MaxXLSXCellCharacters {
		return ErrXLSXBoundsExceeded
	}
	out.WriteString(`<c r="` + ref + `" t="inlineStr"`)
	if style > 0 {
		out.WriteString(` s="` + strconv.Itoa(style) + `"`)
	}
	out.WriteString(`><is><t xml:space="preserve">`)
	writeEscapedXML(out, value)
	out.WriteString(`</t></is></c>`)
	return nil
}

func writeNumberCell(out *strings.Builder, ref string, value float64, style int) {
	writeNumberTextCell(out, ref, strconv.FormatFloat(value, 'f', -1, 64), style)
}

func writeNumberTextCell(out *strings.Builder, ref, value string, style int) {
	out.WriteString(`<c r="` + ref + `" s="` + strconv.Itoa(style) + `"><v>`)
	out.WriteString(value)
	out.WriteString(`</v></c>`)
}

func spreadsheetNumber(value any) (string, bool) {
	var text string
	switch typed := value.(type) {
	case int:
		text = strconv.FormatInt(int64(typed), 10)
	case int8:
		text = strconv.FormatInt(int64(typed), 10)
	case int16:
		text = strconv.FormatInt(int64(typed), 10)
	case int32:
		text = strconv.FormatInt(int64(typed), 10)
	case int64:
		text = strconv.FormatInt(typed, 10)
	case uint:
		text = strconv.FormatUint(uint64(typed), 10)
	case uint8:
		text = strconv.FormatUint(uint64(typed), 10)
	case uint16:
		text = strconv.FormatUint(uint64(typed), 10)
	case uint32:
		text = strconv.FormatUint(uint64(typed), 10)
	case uint64:
		text = strconv.FormatUint(typed, 10)
	case float32:
		text = strconv.FormatFloat(float64(typed), 'f', -1, 32)
	case float64:
		text = strconv.FormatFloat(typed, 'f', -1, 64)
	case json.Number:
		text = typed.String()
	case string:
		text = strings.TrimSpace(typed)
	default:
		return "", false
	}
	number, err := strconv.ParseFloat(text, 64)
	return text, err == nil && !math.IsNaN(number) && !math.IsInf(number, 0)
}

func spreadsheetTime(value any, location *time.Location) (time.Time, bool, bool) {
	var parsed time.Time
	hasTime := false
	switch typed := value.(type) {
	case time.Time:
		parsed = typed
		hasTime = typed.Hour() != 0 || typed.Minute() != 0 || typed.Second() != 0 || typed.Nanosecond() != 0
	case *time.Time:
		if typed == nil {
			return time.Time{}, false, false
		}
		parsed = *typed
		hasTime = typed.Hour() != 0 || typed.Minute() != 0 || typed.Second() != 0 || typed.Nanosecond() != 0
	case string:
		text := strings.TrimSpace(typed)
		if date, err := time.Parse("2006-01-02", text); err == nil {
			return date, false, true
		}
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05Z07:00"} {
			date, err := time.Parse(layout, text)
			if err == nil {
				parsed = date
				hasTime = true
				break
			}
		}
		if parsed.IsZero() {
			date, err := time.ParseInLocation("2006-01-02 15:04:05", text, location)
			if err == nil {
				parsed = date
				hasTime = true
			}
		}
		if parsed.IsZero() {
			return time.Time{}, false, false
		}
	default:
		return time.Time{}, false, false
	}
	localized := parsed.In(location)
	return time.Date(localized.Year(), localized.Month(), localized.Day(), localized.Hour(), localized.Minute(), localized.Second(), localized.Nanosecond(), time.UTC), hasTime, true
}

func spreadsheetDate(value any) (time.Time, bool) {
	var parsed time.Time
	switch typed := value.(type) {
	case time.Time:
		parsed = typed
	case *time.Time:
		if typed == nil {
			return time.Time{}, false
		}
		parsed = *typed
	case string:
		text := strings.TrimSpace(typed)
		for _, layout := range []string{"2006-01-02", time.RFC3339Nano, "2006-01-02 15:04:05Z07:00", "2006-01-02 15:04:05"} {
			date, err := time.Parse(layout, text)
			if err == nil {
				parsed = date
				break
			}
		}
	default:
		return time.Time{}, false
	}
	if parsed.IsZero() {
		return time.Time{}, false
	}
	return time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 0, 0, 0, 0, time.UTC), true
}

func excelSerial(value time.Time) float64 {
	base := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
	return value.Sub(base).Hours() / 24
}

func cellReference(column, row int) string {
	column++
	var label [8]byte
	index := len(label)
	for column > 0 {
		column--
		index--
		label[index] = byte('A' + column%26)
		column /= 26
	}
	return string(label[index:]) + strconv.Itoa(row)
}

func writeEscapedXML(out *strings.Builder, value string) {
	for _, r := range value {
		switch r {
		case '&':
			out.WriteString("&amp;")
		case '<':
			out.WriteString("&lt;")
		case '>':
			out.WriteString("&gt;")
		case '"':
			out.WriteString("&quot;")
		case '\'':
			out.WriteString("&apos;")
		default:
			if r == '\t' || r == '\n' || r == '\r' || r >= 0x20 {
				out.WriteRune(r)
			}
		}
	}
}
