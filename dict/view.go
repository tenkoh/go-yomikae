package dict

import (
	"strings"
	"unicode/utf8"

	"github.com/tenkoh/go-yomikae/internal/script"
	"golang.org/x/text/unicode/norm"
)

// normalize applies NFKC rune by rune, and lowercases when fold is set. Keys
// and views go through the same function so that they compare byte for byte.
func normalize(s string, fold bool) string {
	var b strings.Builder
	for _, r := range s {
		b.WriteString(expand(r, fold))
	}
	return b.String()
}

func expand(r rune, fold bool) string {
	var e string
	if r < utf8.RuneSelf {
		e = string(r)
	} else {
		e = norm.NFKC.String(string(r))
	}
	if fold {
		e = strings.ToLower(e)
	}
	return e
}

// view is the input as the matcher sees it: each original rune replaced by
// its normalized expansion, with maps between view bytes and original runes.
type view struct {
	text   string
	runeAt []int32 // view byte -> index of the rune it came from
	vstart []int   // rune index -> first view byte, plus len(text)
	ostart []int   // rune index -> first original byte, plus len(s)
}

func newView(s string, fold bool) *view {
	v := &view{}
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		e := expand(r, fold)
		ri := int32(len(v.ostart))
		v.ostart = append(v.ostart, i)
		v.vstart = append(v.vstart, b.Len())
		b.WriteString(e)
		for range len(e) {
			v.runeAt = append(v.runeAt, ri)
		}
		i += size
	}
	v.ostart = append(v.ostart, len(s))
	v.vstart = append(v.vstart, b.Len())
	v.text = b.String()
	return v
}

// boundary returns the index of the original rune that starts at view byte
// p, or the rune count when p is the end of the view. It reports false when p
// falls inside an expansion, such as between the k and g of ㎏.
func (v *view) boundary(p int) (int, bool) {
	if p == len(v.text) {
		return len(v.vstart) - 1, true
	}
	ri := int(v.runeAt[p])
	return ri, v.vstart[ri] == p
}

// bounds holds, per original rune, what the literal boundary rule needs.
type bounds struct {
	wordy []bool // a letter, a mark on a letter, or an apostrophe between letters
	opens []bool // a letter, a mark, or an apostrophe followed by a letter
}

func newBounds(s string) *bounds {
	var rs []rune
	for _, r := range s {
		rs = append(rs, r)
	}
	b := &bounds{wordy: make([]bool, len(rs)), opens: make([]bool, len(rs))}
	for i, r := range rs {
		nextLetter := i+1 < len(rs) && script.IsLetter(rs[i+1])
		switch {
		case script.IsLetter(r):
			b.wordy[i], b.opens[i] = true, true
		case script.IsMark(r):
			b.wordy[i], b.opens[i] = i > 0 && b.wordy[i-1], true
		case script.IsApostrophe(r):
			b.wordy[i], b.opens[i] = i > 0 && b.wordy[i-1] && nextLetter, nextLetter
		}
	}
	return b
}

// allowed reports whether a literal spanning runes [a, b) may match: a key
// that starts with a letter must not continue a word, and a key that ends
// with a letter must not run into one. Digits may touch either end.
func (bd *bounds) allowed(a, b int, firstLetter, lastLetter bool) bool {
	if firstLetter && a > 0 && bd.wordy[a-1] {
		return false
	}
	if lastLetter && b < len(bd.opens) && bd.opens[b] {
		return false
	}
	return true
}
