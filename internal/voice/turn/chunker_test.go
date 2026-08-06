package turn

import (
	"errors"
	"strings"
	"testing"
)

func TestSpeechChunkerEmitsEnglishAndIndicSentencesIncrementally(t *testing.T) {
	chunker := NewSpeechChunker()

	chunks, err := chunker.Write("Your invoice is ready!")
	if err != nil {
		t.Fatalf("Write(English) error = %v", err)
	}
	if len(chunks) != 1 || chunks[0] != "Your invoice is ready!" {
		t.Fatalf("English chunks = %#v", chunks)
	}

	chunks, err = chunker.Write(" आपका चालान तैयार है। अगली राशि कल देय है")
	if err != nil {
		t.Fatalf("Write(Indic) error = %v", err)
	}
	if len(chunks) != 1 || chunks[0] != "आपका चालान तैयार है।" {
		t.Fatalf("Indic chunks = %#v", chunks)
	}

	chunks, err = chunker.Flush()
	if err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if len(chunks) != 1 || chunks[0] != "अगली राशि कल देय है" {
		t.Fatalf("final chunks = %#v", chunks)
	}
}

func TestSpeechChunkerDoesNotSplitAbbreviationsOrNumbers(t *testing.T) {
	chunker := NewSpeechChunker()
	chunks, err := chunker.Write("Dr.")
	if err != nil {
		t.Fatalf("Write(abbreviation) error = %v", err)
	}
	if len(chunks) != 0 {
		t.Fatalf("abbreviation chunks = %#v, want none", chunks)
	}

	const spoken = "Dr. Mehta approved invoice INV-42 for 12.50 rupees today."
	chunks, err = chunker.Write(" Mehta approved invoice INV-42 for 12.50 rupees today.")
	if err != nil {
		t.Fatalf("Write(remainder) error = %v", err)
	}
	if len(chunks) != 1 || chunks[0] != spoken {
		t.Fatalf("chunks = %#v, want %q", chunks, spoken)
	}
}

func TestSpeechChunkerUsesPhraseBoundariesWithinLatencyTarget(t *testing.T) {
	words := []string{
		"please", "review", "the", "latest", "invoice", "summary", "for", "the", "north", "branch",
		"and", "confirm", "whether", "the", "customer", "balance", "matches", "the", "validated", "ledger",
		"before", "sending", "a", "short", "spoken", "response", "to", "the", "caller", "with", "the",
		"final", "payment", "status", "and", "the", "next", "due", "date", "for", "follow", "up",
	}
	input := strings.Join(words, " ")
	chunker := NewSpeechChunker()
	chunks, err := chunker.Write(input)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	tail, err := chunker.Flush()
	if err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	chunks = append(chunks, tail...)
	if len(chunks) < 2 {
		t.Fatalf("chunks = %#v, want an early phrase chunk", chunks)
	}
	for _, chunk := range chunks {
		if length := len([]rune(chunk)); length == 0 || length > speechChunkTargetMaxRunes {
			t.Fatalf("chunk length = %d, want 1..%d: %q", length, speechChunkTargetMaxRunes, chunk)
		}
	}
	if got := strings.Join(chunks, " "); got != input {
		t.Fatalf("rejoined chunks = %q, want %q", got, input)
	}
}

func TestSpeechChunkerNeverExceedsProviderHardLimit(t *testing.T) {
	input := strings.Repeat("क", speechChunkHardMaxRunes*2+1)
	chunker := NewSpeechChunker()
	chunks, err := chunker.Write(input)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	tail, err := chunker.Flush()
	if err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	chunks = append(chunks, tail...)
	if len(chunks) != 3 {
		t.Fatalf("chunk count = %d, want 3", len(chunks))
	}
	for _, chunk := range chunks {
		if length := len([]rune(chunk)); length < 1 || length > speechChunkHardMaxRunes {
			t.Fatalf("chunk length = %d, hard max = %d", length, speechChunkHardMaxRunes)
		}
	}
	if got := strings.Join(chunks, ""); got != input {
		t.Fatalf("chunked text was changed")
	}
}

func TestSpeechChunkerRejectsNonSpokenAndUnsafeSynthesis(t *testing.T) {
	tests := []struct {
		name   string
		parts  []string
		wanted error
	}{
		{name: "invalid UTF-8", parts: []string{string([]byte{0xff})}, wanted: ErrInvalidSpeechText},
		{name: "control", parts: []string{"hello\x00world"}, wanted: ErrInvalidSpeechText},
		{name: "markdown", parts: []string{"**paid**"}, wanted: ErrUnsafeSpeechText},
		{name: "code fence", parts: []string{"```go"}, wanted: ErrUnsafeSpeechText},
		{name: "table", parts: []string{"invoice | status"}, wanted: ErrUnsafeSpeechText},
		{name: "split URL", parts: []string{"Read htt", "ps://example.com now."}, wanted: ErrUnsafeSpeechText},
		{name: "hidden reasoning", parts: []string{"Chain of thought: first inspect the ledger."}, wanted: ErrUnsafeSpeechText},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			chunker := NewSpeechChunker()
			var err error
			for _, part := range test.parts {
				_, err = chunker.Write(part)
				if err != nil {
					break
				}
			}
			if !errors.Is(err, test.wanted) {
				t.Fatalf("Write() error = %v, want %v", err, test.wanted)
			}
			if chunks, flushErr := chunker.Flush(); len(chunks) != 0 || !errors.Is(flushErr, test.wanted) {
				t.Fatalf("Flush() after rejection = (%#v, %v)", chunks, flushErr)
			}
		})
	}
}
