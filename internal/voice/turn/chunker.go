package turn

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	speechChunkTargetMinRunes = 80
	speechChunkTargetMaxRunes = 220
	speechChunkHardMaxRunes   = 500
	speechTextMaximumBytes    = 32 << 10
)

var (
	ErrInvalidSpeechText = errors.New("voice speech text is invalid")
	ErrUnsafeSpeechText  = errors.New("voice speech text is unsafe for synthesis")
)

// SpeechChunker incrementally turns streamed model text into bounded spoken
// phrases. It retains the accepted response for cross-delta safety checks, but
// emits sentence boundaries immediately so TTS can produce first audio early.
type SpeechChunker struct {
	pending []rune
	checked strings.Builder
	closed  bool
	err     error
}

func NewSpeechChunker() *SpeechChunker { return &SpeechChunker{} }

func (chunker *SpeechChunker) Write(delta string) ([]string, error) {
	if chunker == nil || chunker.closed {
		return nil, ErrInvalidSpeechText
	}
	if chunker.err != nil {
		return nil, chunker.err
	}
	if delta == "" || !utf8.ValidString(delta) || chunker.checked.Len()+len(delta) > speechTextMaximumBytes {
		chunker.err = ErrInvalidSpeechText
		return nil, chunker.err
	}
	for _, character := range delta {
		if character == 0 || (unicode.IsControl(character) && character != '\n' && character != '\r' && character != '\t') {
			chunker.err = ErrInvalidSpeechText
			return nil, chunker.err
		}
	}
	chunker.checked.WriteString(delta)
	if unsafeSpeechText(chunker.checked.String()) {
		chunker.err = ErrUnsafeSpeechText
		clear(chunker.pending)
		chunker.pending = nil
		return nil, chunker.err
	}
	chunker.appendNormalized(delta)
	return chunker.extract(false), nil
}

func (chunker *SpeechChunker) Flush() ([]string, error) {
	if chunker == nil {
		return nil, ErrInvalidSpeechText
	}
	if chunker.err != nil {
		return nil, chunker.err
	}
	if chunker.closed {
		return nil, ErrInvalidSpeechText
	}
	chunker.closed = true
	chunks := chunker.extract(true)
	if len(chunks) == 0 && strings.TrimSpace(chunker.checked.String()) == "" {
		return nil, ErrInvalidSpeechText
	}
	return chunks, nil
}

func (chunker *SpeechChunker) appendNormalized(delta string) {
	for _, character := range delta {
		if unicode.IsSpace(character) {
			if len(chunker.pending) == 0 || chunker.pending[len(chunker.pending)-1] != ' ' {
				chunker.pending = append(chunker.pending, ' ')
			}
			continue
		}
		chunker.pending = append(chunker.pending, character)
	}
}

func (chunker *SpeechChunker) extract(flush bool) []string {
	chunks := make([]string, 0, 2)
	for {
		chunker.trimPendingLeft()
		if len(chunker.pending) == 0 {
			return chunks
		}

		boundary := sentenceBoundary(chunker.pending, speechChunkTargetMaxRunes)
		if boundary < 0 {
			boundary = phraseBoundary(chunker.pending, speechChunkTargetMinRunes, speechChunkTargetMaxRunes)
		}
		if boundary < 0 && len(chunker.pending) >= speechChunkTargetMaxRunes {
			boundary = whitespaceBoundary(chunker.pending, speechChunkTargetMinRunes, speechChunkTargetMaxRunes)
		}
		if boundary < 0 && len(chunker.pending) > speechChunkHardMaxRunes {
			boundary = whitespaceBoundary(chunker.pending, speechChunkTargetMinRunes, speechChunkHardMaxRunes)
			if boundary < 0 {
				boundary = speechChunkHardMaxRunes - 1
			}
		}
		if boundary < 0 && flush {
			if len(chunker.pending) > speechChunkHardMaxRunes {
				boundary = speechChunkHardMaxRunes - 1
			} else {
				boundary = len(chunker.pending) - 1
			}
		}
		if boundary < 0 {
			return chunks
		}

		chunk := strings.TrimSpace(string(chunker.pending[:boundary+1]))
		clear(chunker.pending[:boundary+1])
		chunker.pending = chunker.pending[boundary+1:]
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
	}
}

func (chunker *SpeechChunker) trimPendingLeft() {
	index := 0
	for index < len(chunker.pending) && unicode.IsSpace(chunker.pending[index]) {
		index++
	}
	if index != 0 {
		clear(chunker.pending[:index])
		chunker.pending = chunker.pending[index:]
	}
}

func sentenceBoundary(text []rune, maximum int) int {
	limit := min(len(text), maximum)
	for index := 0; index < limit; index++ {
		character := text[index]
		if character == '.' && !periodEndsSentence(text, index) {
			continue
		}
		if character != '.' && character != '!' && character != '?' && character != '।' && character != '॥' && character != '؟' {
			continue
		}
		for index+1 < limit && isClosingSpeechPunctuation(text[index+1]) {
			index++
		}
		return index
	}
	return -1
}

func periodEndsSentence(text []rune, index int) bool {
	if index > 0 && index+1 < len(text) && unicode.IsDigit(text[index-1]) && unicode.IsDigit(text[index+1]) {
		return false
	}
	start := index
	for start > 0 && (unicode.IsLetter(text[start-1]) || text[start-1] == '.') {
		start--
	}
	token := strings.ToLower(string(text[start : index+1]))
	if _, abbreviation := speechAbbreviations[token]; abbreviation {
		return false
	}
	letters := 0
	for _, character := range token {
		if unicode.IsLetter(character) {
			letters++
		}
	}
	return !(letters == 1 && len(token) == 2)
}

func phraseBoundary(text []rune, minimum, maximum int) int {
	limit := min(len(text), maximum)
	for index := minimum - 1; index < limit; index++ {
		switch text[index] {
		case ',', '،':
			if index > 0 && index+1 < len(text) && unicode.IsDigit(text[index-1]) && unicode.IsDigit(text[index+1]) {
				continue
			}
			return index
		case ';', ':':
			return index
		}
	}
	return -1
}

func whitespaceBoundary(text []rune, minimum, maximum int) int {
	limit := min(len(text), maximum)
	for index := limit - 1; index >= minimum; index-- {
		if unicode.IsSpace(text[index]) {
			return index
		}
	}
	return -1
}

func isClosingSpeechPunctuation(character rune) bool {
	return character == '"' || character == '\'' || character == '”' || character == '’' ||
		character == ')' || character == ']' || character == '}'
}

func unsafeSpeechText(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{
		"http://", "https://", "www.", "```", "~~~", "`", "|", "**", "__", "![", "](",
		"<think", "</think", "chain of thought", "hidden reasoning",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	for _, line := range strings.Split(lower, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") || strings.HasPrefix(line, "##") || strings.HasPrefix(line, "> ") ||
			strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "+ ") {
			return true
		}
	}
	for _, token := range strings.Fields(lower) {
		token = strings.Trim(token, "()[]{}<>,;:!?\"'")
		if strings.Contains(token, "@") {
			return true
		}
		for _, suffix := range []string{".com", ".org", ".net", ".in", ".ai", ".io"} {
			if strings.HasSuffix(token, suffix) || strings.Contains(token, suffix+"/") {
				return true
			}
		}
	}
	return false
}

var speechAbbreviations = map[string]struct{}{
	"dr.": {}, "mr.": {}, "mrs.": {}, "ms.": {}, "prof.": {}, "sr.": {}, "jr.": {},
	"vs.": {}, "etc.": {}, "e.g.": {}, "i.e.": {}, "a.m.": {}, "p.m.": {},
}
