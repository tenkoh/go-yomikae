// Package script decides which runes are target letters and how to read them.
// It is the single source of truth for what go-yomikae removes from text.
package script

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// scripts lists the scripts whose letters are targets. Greek is here because
// IrodoriTTS misreads Greek letters; to leave Greek untouched, delete it here
// and its table greekSpell.
var scripts = []*unicode.RangeTable{unicode.Latin, unicode.Greek}

func isScriptLetter(r rune) bool {
	if r < utf8.RuneSelf {
		return 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z'
	}
	return unicode.IsLetter(r) && unicode.IsOneOf(scripts, r)
}

// IsLetter reports whether r is a target letter: a Latin or Greek letter, or a
// rune whose NFKC form contains one, such as ＰＤＦ, ㎏, ™, ℃ or Ⅲ.
func IsLetter(r rune) bool {
	if isScriptLetter(r) {
		return true
	}
	if r < utf8.RuneSelf {
		return false
	}
	s := string(r)
	if norm.NFKC.IsNormalString(s) {
		return false
	}
	for _, c := range norm.NFKC.String(s) {
		if isScriptLetter(c) {
			return true
		}
	}
	return false
}

// HasLetter reports whether s contains a target letter. Invalid UTF-8 bytes
// are not letters.
func HasLetter(s string) bool {
	for _, r := range s {
		if IsLetter(r) {
			return true
		}
	}
	return false
}

// IsMark reports whether r is a combining mark. A mark that follows a letter
// belongs to it and is removed with it.
func IsMark(r rune) bool { return unicode.Is(unicode.M, r) }

// IsApostrophe reports whether r is an apostrophe that may join two letters,
// as in don't or Python's.
func IsApostrophe(r rune) bool { return r == '\'' || r == '’' }

// Case is the letter case of a rune, judged on its NFKC form.
type Case uint8

const (
	NoCase Case = iota // caseless, mixed (㎑ is kHz) or not a letter
	Upper
	Lower
)

// CaseOf returns the case of r. Upper and Lower require every letter of the
// NFKC form to have that case.
func CaseOf(r rune) Case {
	if r < utf8.RuneSelf {
		switch {
		case 'a' <= r && r <= 'z':
			return Lower
		case 'A' <= r && r <= 'Z':
			return Upper
		}
		return NoCase
	}
	c := NoCase
	for _, x := range nfkc(r) {
		if !unicode.IsLetter(x) {
			continue
		}
		var xc Case
		switch {
		case unicode.IsUpper(x):
			xc = Upper
		case unicode.IsLower(x):
			xc = Lower
		default:
			return NoCase
		}
		if c != NoCase && c != xc {
			return NoCase
		}
		c = xc
	}
	return c
}

// Letters counts the letters in the NFKC form of s and reports whether all of
// them are upper case. ㎏ counts as two letters.
func Letters(s string) (n int, allUpper bool) {
	allUpper = true
	for _, r := range s {
		for _, x := range nfkc(r) {
			if !unicode.IsLetter(x) {
				continue
			}
			n++
			if !unicode.IsUpper(x) {
				allUpper = false
			}
		}
	}
	return n, allUpper
}

func nfkc(r rune) string {
	s := string(r)
	if r < utf8.RuneSelf || norm.NFKC.IsNormalString(s) {
		return s
	}
	return norm.NFKC.String(s)
}

// special maps letters that NFD does not reduce to ASCII.
var special = map[rune]string{
	'ß': "ss", 'æ': "ae", 'œ': "oe", 'ø': "o", 'ł': "l",
	'đ': "d", 'ð': "d", 'þ': "th", 'ı': "i",
}

// Fold returns s as lowercase ASCII letters for the C2K model: café is cafe,
// ß is ss, and apostrophes and combining marks are dropped. It reports false
// when anything else remains, such as Greek, ə or the 2 of ㎡.
func Fold(s string) (string, bool) {
	var b strings.Builder
	for _, r := range norm.NFKC.String(s) {
		r = unicode.ToLower(r)
		if sp, ok := special[r]; ok {
			b.WriteString(sp)
			continue
		}
		for _, x := range norm.NFD.String(string(r)) {
			switch {
			case 'a' <= x && x <= 'z':
				b.WriteRune(x)
			case unicode.Is(unicode.Mn, x) || x == 'ʼ' || IsApostrophe(x):
			default:
				return "", false
			}
		}
	}
	if b.Len() == 0 {
		return "", false
	}
	return b.String(), true
}

// Spell reads s letter by letter. It never fails: letters without a reading,
// apostrophes, marks and symbols are dropped, and digits from NFKC (㎡ is m2)
// are kept. The result has no target letter and may be empty.
func Spell(s string) string {
	var b strings.Builder
	for _, r := range s {
		if IsMark(r) || IsApostrophe(r) {
			continue
		}
		for _, x := range nfkc(r) {
			switch {
			case '0' <= x && x <= '9':
				b.WriteRune(x)
			case isGreek(x):
				base, _ := utf8.DecodeRuneInString(norm.NFD.String(string(unicode.ToLower(x))))
				b.WriteString(greekSpell[base])
			case unicode.IsLetter(x):
				f, ok := Fold(string(x))
				if !ok {
					continue
				}
				for i := 0; i < len(f); i++ {
					b.WriteString(latinSpell[f[i]-'a'])
				}
			}
		}
	}
	return b.String()
}

// latinSpell follows the spell table of upstream e2k.
var latinSpell = [26]string{
	"エー", "ビー", "シー", "ディー", "イー", "エフ", "ジー", "エイチ", "アイ", "ジェー",
	"ケー", "エル", "エム", "エヌ", "オー", "ピー", "キュー", "アール", "エス", "ティー",
	"ユー", "ヴィー", "ダブリュー", "エックス", "ワイ", "ゼット",
}

func isGreek(r rune) bool { return unicode.Is(unicode.Greek, r) && unicode.IsLetter(r) }

// greekSpell reads lowercase Greek letters without diacritics. Letters
// missing here, such as archaic digamma, are dropped by Spell.
var greekSpell = map[rune]string{
	'α': "アルファ", 'β': "ベータ", 'γ': "ガンマ", 'δ': "デルタ", 'ε': "イプシロン",
	'ζ': "ゼータ", 'η': "イータ", 'θ': "シータ", 'ι': "イオタ", 'κ': "カッパ",
	'λ': "ラムダ", 'μ': "ミュー", 'ν': "ニュー", 'ξ': "クサイ", 'ο': "オミクロン",
	'π': "パイ", 'ρ': "ロー", 'σ': "シグマ", 'ς': "シグマ", 'τ': "タウ",
	'υ': "ユプシロン", 'φ': "ファイ", 'χ': "カイ", 'ψ': "プサイ", 'ω': "オメガ",
}
