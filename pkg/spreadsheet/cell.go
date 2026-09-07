package spreadsheet

import "strings"

// SafeCell neutralizes formula-capable strings while preserving every
// non-string value so money, numbers, booleans and dates retain native types.
func SafeCell(value any) any {
	text, ok := value.(string)
	if !ok || text == "" {
		return value
	}
	trimmed := strings.TrimLeft(text, " \r\n")
	if trimmed == "" {
		return text
	}
	switch trimmed[0] {
	case '=', '+', '-', '@', '\t':
		return "'" + text
	default:
		return text
	}
}
