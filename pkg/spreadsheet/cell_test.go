package spreadsheet

import (
	"testing"
	"time"
)

func TestSafeCellNeutralizesStringsAndPreservesTypedCells(t *testing.T) {
	date := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct{ input, want any }{
		{"=1+1", "'=1+1"}, {" +SUM(1,2)", "' +SUM(1,2)"}, {"ordinary", "ordinary"},
		{int64(42), int64(42)}, {12.5, 12.5}, {true, true}, {date, date},
	} {
		if got := SafeCell(test.input); got != test.want {
			t.Fatalf("SafeCell(%#v)=%#v want %#v", test.input, got, test.want)
		}
	}
}
