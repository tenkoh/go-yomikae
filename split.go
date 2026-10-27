package yomikae

import (
	"unicode"
	"unicode/utf8"

	"github.com/tenkoh/go-yomikae/internal/script"
)

// kind is the role of a rune in token extraction.
type kind uint8

const (
	other  kind = iota // separates tokens
	letter             // a target letter
	digit
	joiner // a mark after a letter, or an apostrophe between letters
)

type runeInfo struct {
	off int
	r   rune
	k   kind
}

// piece is a run of letters in a token, read as one unit.
type piece struct {
	start, end int // byte range in the input
	class      Class
}

// pieces returns the letter pieces of the tokens in s[start:end]. A token is
// a run of letters, digits and joiners with at least one letter. Tokens split
// between letters and digits, from lower to upper case (getUser is get|User),
// and before the last of a run of capitals followed by a lowercase letter
// (HTTPServer is HTTP|Server). Digit pieces are left as they are.
func pieces(s string, start, end int) []piece {
	var rs []runeInfo
	for i := start; i < end; {
		r, size := utf8.DecodeRuneInString(s[i:end])
		rs = append(rs, runeInfo{off: i, r: r})
		i += size
	}
	for i := range rs {
		r := rs[i].r
		switch {
		case script.IsLetter(r):
			rs[i].k = letter
		case unicode.IsDigit(r):
			rs[i].k = digit
		case script.IsMark(r):
			if i > 0 && (rs[i-1].k == letter || rs[i-1].k == joiner) {
				rs[i].k = joiner
			}
		case script.IsApostrophe(r):
			if i > 0 && (rs[i-1].k == letter || rs[i-1].k == joiner) && i+1 < len(rs) && script.IsLetter(rs[i+1].r) {
				rs[i].k = joiner
			}
		}
	}
	offset := func(i int) int {
		if i == len(rs) {
			return end
		}
		return rs[i].off
	}

	var out []piece
	emit := func(i, j int) {
		src := s[offset(i):offset(j)]
		n, upper := script.Letters(src)
		class := Word
		switch {
		case n <= 1:
			class = Letter
		case upper:
			class = Acronym
		}
		out = append(out, piece{start: offset(i), end: offset(j), class: class})
	}
	for i := 0; i < len(rs); {
		if rs[i].k == other || rs[i].k == digit {
			i++
			continue
		}
		// A letter run: letters and joiners up to a digit or a separator.
		j := i
		for j < len(rs) && (rs[j].k == letter || rs[j].k == joiner) {
			j++
		}
		cut, prev := i, -1
		for x := i; x < j; x++ {
			if rs[x].k != letter {
				continue
			}
			if prev >= 0 && script.CaseOf(rs[x].r) == script.Upper {
				switch script.CaseOf(rs[prev].r) {
				case script.Lower:
					emit(cut, x)
					cut = x
				case script.Upper:
					if next := nextLetter(rs, x+1, j); next >= 0 && script.CaseOf(rs[next].r) == script.Lower {
						emit(cut, x)
						cut = x
					}
				}
			}
			prev = x
		}
		emit(cut, j)
		i = j
	}
	return out
}

func nextLetter(rs []runeInfo, from, to int) int {
	for i := from; i < to; i++ {
		if rs[i].k == letter {
			return i
		}
	}
	return -1
}
