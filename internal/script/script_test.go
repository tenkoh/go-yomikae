package script

import "testing"

func TestIsLetter(t *testing.T) {
	letters := "aZｚＰéßø𝙈㎏™℃Ⅲⓐﬁαλ Ωµ"
	for _, r := range letters {
		if r == ' ' {
			continue
		}
		if !IsLetter(r) {
			t.Errorf("IsLetter(%q) = false, want true", r)
		}
	}
	for _, r := range "1１あア漢ーПр한́'’.%°²�" {
		if IsLetter(r) {
			t.Errorf("IsLetter(%q) = true, want false", r)
		}
	}
}

func TestFold(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"Python", "python", true},
		{"café", "cafe", true},
		{"café", "cafe", true},
		{"ＰＤＦ", "pdf", true},
		{"Straße", "strasse", true},
		{"Æsir", "aesir", true},
		{"Œuvre", "oeuvre", true},
		{"Ørsted", "orsted", true},
		{"Łódź", "lodz", true},
		{"Đorđe", "dorde", true},
		{"Þór", "thor", true},
		{"ı", "i", true},
		{"Python's", "pythons", true},
		{"don’t", "dont", true},
		{"ﬁle", "file", true},
		{"㎏", "kg", true},
		{"㎡", "", false},
		{"λόγος", "", false},
		{"ə", "", false},
		{"'", "", false},
	}
	for _, tt := range tests {
		got, ok := Fold(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("Fold(%q) = %q, %v, want %q, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestSpell(t *testing.T) {
	tests := []struct{ in, want string }{
		{"API", "エーピーアイ"},
		{"wxyz", "ダブリューエックスワイゼット"},
		{"ＰＤＦ", "ピーディーエフ"},
		{"α", "アルファ"},
		{"Ω", "オメガ"},
		{"ΔT", "デルタティー"},
		{"ά", "アルファ"},
		{"ς", "シグマ"},
		{"µ", "ミュー"},
		{"é", "イー"},
		{"ß", "エスエス"},
		{"㎡", "エム2"},
		{"℃", "シー"},
		{"Python's", "ピーワイティーエイチオーエヌエス"},
		{"ə", ""},
		{"ϝ", ""},
	}
	for _, tt := range tests {
		got := Spell(tt.in)
		if got != tt.want {
			t.Errorf("Spell(%q) = %q, want %q", tt.in, got, tt.want)
		}
		if HasLetter(got) {
			t.Errorf("Spell(%q) = %q has a letter", tt.in, got)
		}
	}
}

func TestCaseOf(t *testing.T) {
	tests := []struct {
		r    rune
		want Case
	}{
		{'a', Lower}, {'A', Upper}, {'Ｐ', Upper}, {'ｐ', Lower}, {'Σ', Upper},
		{'σ', Lower}, {'㎑', NoCase}, {'㎏', Lower}, {'1', NoCase}, {'́', NoCase},
	}
	for _, tt := range tests {
		if got := CaseOf(tt.r); got != tt.want {
			t.Errorf("CaseOf(%q) = %v, want %v", tt.r, got, tt.want)
		}
	}
}
