package yomikae

import (
	"bufio"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tenkoh/e2k-go"
	"github.com/tenkoh/go-yomikae/internal/script"
	"github.com/tenkoh/go-yomikae/ngram"
)

func corpusLines(t testing.TB) []string {
	t.Helper()
	files, err := filepath.Glob("testdata/corpus/*.txt")
	if err != nil || len(files) == 0 {
		t.Fatalf("no corpus files: %v", err)
	}
	var lines []string
	for _, name := range files {
		f, err := os.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			lines = append(lines, sc.Text())
		}
		f.Close()
		if err := sc.Err(); err != nil {
			t.Fatal(err)
		}
	}
	return lines
}

func TestCorpusHasNoLetters(t *testing.T) {
	dicts := builtin(t)
	for _, opts := range []Options{
		{Dictionaries: dicts},
		{Dictionaries: dicts, Model: kanaModel{}},
		{Dictionaries: dicts, Model: hostileModel{}},
	} {
		c := newConverter(t, opts)
		for _, line := range corpusLines(t) {
			r := convert(t, c, line)
			if script.HasLetter(r.Text) {
				t.Errorf("Convert(%q) = %q has a letter", line, r.Text)
			}
			checkRewrites(t, line, r)
		}
	}
}

func TestSpans(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"https://go.dev/doc を見る", "https://go.dev/doc:url"},
		{"a@example.com に送る", "a@example.com:url"},
		{"/var/log/app と CI/CD", "/var/log/app:path CI/CD:path"},
		{"config.yamlとsnake_caseとWi-FiとuseState", "config.yaml:identifier:mixed snake_case:identifier:mixed Wi-Fi:identifier:mixed useState:identifier:mixed"},
		{"S3や2.4GHzやv1.2", "S3:alnum:mixed 2.4GHz:alnum:mixed v1.2:alnum:mixed"},
		{"APIとGoとx", "API:acronym:mixed Go:word:mixed x:letter:mixed"},
		{"Go. 次に", "Go:word"},
		{"ＰＤＦを", "ＰＤＦ:acronym:mixed"},
		{"123 と 日本語", ""},
	}
	for _, tt := range tests {
		var got []string
		for _, sp := range Spans(tt.in) {
			s := tt.in[sp.Start:sp.End] + ":" + string(sp.Kind)
			if sp.Mixed {
				s += ":mixed"
			}
			got = append(got, s)
		}
		if s := strings.Join(got, " "); s != tt.want {
			t.Errorf("Spans(%q) = %q, want %q", tt.in, s, tt.want)
		}
	}
}

// realConverter builds a Converter from the C2K weights named by
// YOMIKAE_C2K_MODEL, the bundled NGram data and the builtin dictionaries.
func realConverter(t *testing.T) *Converter {
	t.Helper()
	path := os.Getenv("YOMIKAE_C2K_MODEL")
	if path == "" {
		t.Skip("YOMIKAE_C2K_MODEL is not set")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	model, err := e2k.LoadC2K(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	judge, err := ngram.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	return newConverter(t, Options{Dictionaries: builtin(t), Model: model, Judge: judge})
}

// TestRealModel runs the real model over the corpus.
func TestRealModel(t *testing.T) {
	c := realConverter(t)

	tests := []struct{ in, want string }{
		{"updateする", "アップデートする"},
		{"npm installで", "エヌピーエム インストールで"},
		{"GitHubとJSON", "ギットハブとジェイソン"},
		{"10gの塩", "10グラムの塩"},
	}
	for _, tt := range tests {
		if r := convert(t, c, tt.in); r.Text != tt.want {
			t.Errorf("Convert(%q) = %q, want %q", tt.in, r.Text, tt.want)
		}
	}
	for _, line := range corpusLines(t) {
		r := convert(t, c, line)
		if script.HasLetter(r.Text) {
			t.Errorf("Convert(%q) = %q has a letter", line, r.Text)
		}
	}
}

// TestGold reports how many readings in testdata/gold.tsv match an acceptable
// reading. The design asks for at least 70%.
func TestGold(t *testing.T) {
	c := realConverter(t)
	data, err := os.ReadFile("testdata/gold.tsv")
	if err != nil {
		t.Fatal(err)
	}
	var total, ok int
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		in, want, _ := strings.Cut(line, "\t")
		got := convert(t, c, in).Text
		total++
		if slices.Contains(strings.Split(want, "|"), got) {
			ok++
		} else {
			t.Logf("%s → %s, want %s", in, got, want)
		}
	}
	rate := float64(ok) / float64(total)
	t.Logf("gold: %d/%d acceptable (%.0f%%)", ok, total, 100*rate)
	if rate < 0.7 {
		t.Errorf("gold rate %.2f is below 0.70", rate)
	}
}
