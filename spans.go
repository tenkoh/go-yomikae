package yomikae

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tenkoh/go-yomikae/internal/script"
)

// SpanKind is the shape of a span of text around letters. It is for
// statistics and debugging; conversion does not depend on it.
type SpanKind string

const (
	SpanURL        SpanKind = "url"        // https://go.dev/doc, a@example.com
	SpanPath       SpanKind = "path"       // /var/log, CI/CD
	SpanIdentifier SpanKind = "identifier" // snake_case, kebab-case, go.mod, camelCase
	SpanAlnum      SpanKind = "alnum"      // S3, 2.4GHz, v1.2
	SpanAcronym    SpanKind = "acronym"    // API
	SpanWord       SpanKind = "word"       // update, Go
	SpanLetter     SpanKind = "letter"     // x
)

// Span is a run of letters, digits and connecting symbols that contains a
// letter, delimited by spaces, Japanese and other punctuation.
type Span struct {
	Start, End int // byte range in the input
	Kind       SpanKind
	Mixed      bool // touches Japanese text on either side
}

const spanSymbols = "._-/\\:@#~+%?=&'’"

func isSpanRune(r rune) bool {
	return script.IsLetter(r) || unicode.IsDigit(r) || script.IsMark(r) || strings.ContainsRune(spanSymbols, r)
}

func isJapanese(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana) || r == 'ー'
}

// Spans returns the spans of s in order.
func Spans(s string) []Span {
	var out []Span
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !isSpanRune(r) {
			i += size
			continue
		}
		j := i + size
		for j < len(s) {
			r, size := utf8.DecodeRuneInString(s[j:])
			if !isSpanRune(r) {
				break
			}
			j += size
		}
		start, end := i, j
		i = j
		// Sentence punctuation after a span is not part of it.
		for end > start {
			r, size := utf8.DecodeLastRuneInString(s[start:end])
			if !strings.ContainsRune(".:?'’", r) {
				break
			}
			end -= size
		}
		t := s[start:end]
		if !script.HasLetter(t) {
			continue
		}
		before, _ := utf8.DecodeLastRuneInString(s[:start])
		after, _ := utf8.DecodeRuneInString(s[end:])
		out = append(out, Span{
			Start: start,
			End:   end,
			Kind:  spanKind(t),
			Mixed: isJapanese(before) || isJapanese(after),
		})
	}
	return out
}

func spanKind(t string) SpanKind {
	switch {
	case strings.Contains(t, "://") || strings.HasPrefix(t, "www.") || isEmail(t):
		return SpanURL
	case strings.ContainsAny(t, "/\\"):
		return SpanPath
	case isIdentifier(t):
		return SpanIdentifier
	case strings.IndexFunc(t, unicode.IsDigit) >= 0:
		return SpanAlnum
	}
	switch n, upper := script.Letters(t); {
	case n == 1:
		return SpanLetter
	case upper:
		return SpanAcronym
	}
	return SpanWord
}

func isEmail(t string) bool {
	at := strings.IndexByte(t, '@')
	return at > 0 && strings.Contains(t[at+1:], ".")
}

// isIdentifier reports snake_case, kebab-case, dotted names and camelCase.
func isIdentifier(t string) bool {
	if strings.ContainsAny(t, "_-") {
		return true
	}
	var prev rune
	for _, r := range t {
		if prev != 0 {
			switch {
			case r == '.' && script.IsLetter(prev):
				return true
			case script.CaseOf(prev) == script.Lower && script.CaseOf(r) == script.Upper:
				return true
			}
		}
		prev = r
	}
	return false
}
