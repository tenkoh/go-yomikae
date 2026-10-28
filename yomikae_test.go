package yomikae

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/tenkoh/go-yomikae/dict"
	"github.com/tenkoh/go-yomikae/internal/script"
)

// kanaModel is a fake Transliterator that reads each letter of a word as one
// katakana, so that tests can tell its output from spelling.
type kanaModel struct{}

const kanaTable = "アイウエオカキクケコサシスセソタチツテトナニヌネノハ"

func (kanaModel) ConvertContext(ctx context.Context, s string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return kanaOf(s), nil
}

func kanaOf(s string) string {
	kana := []rune(kanaTable)
	var b strings.Builder
	for _, c := range s {
		if 'a' <= c && c <= 'z' {
			b.WriteRune(kana[c-'a'])
		}
	}
	return b.String()
}

// hostileModel returns, depending on the word, Latin text, nothing, invalid
// UTF-8, kana mixed with Latin, a lone combining mark, or an error.
type hostileModel struct{}

func (hostileModel) ConvertContext(ctx context.Context, s string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	h := fnv.New32a()
	h.Write([]byte(s))
	switch h.Sum32() % 7 {
	case 0:
		return s, nil
	case 1:
		return "", nil
	case 2:
		return "\xc3", nil
	case 3:
		return "ア" + s, nil
	case 4:
		return "́", nil
	case 5:
		return "", errors.New("model failed")
	}
	return "<pad>", nil
}

func newConverter(t testing.TB, opts Options) *Converter {
	t.Helper()
	c, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func convert(t testing.TB, c *Converter, s string) Result {
	t.Helper()
	r, err := c.Convert(context.Background(), s)
	if err != nil {
		t.Fatalf("Convert(%q): %v", s, err)
	}
	return r
}

func builtin(t testing.TB) []*dict.Dictionary {
	t.Helper()
	ds, err := dict.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

func mustDict(t testing.TB, src string) *dict.Dictionary {
	t.Helper()
	d, err := dict.Load("test.toml", strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestPieces(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"iPhone", "i:letter Phone:word"},
		{"HTTPServer", "HTTP:acronym Server:word"},
		{"getUserById", "get:word User:word By:word Id:word"},
		{"DynamoDB", "Dynamo:word DB:acronym"},
		{"macOS", "mac:word OS:acronym"},
		{"H2O", "H:letter O:letter"},
		{"mp3", "mp:word"},
		{"v1.2", "v:letter"},
		{"5G", "G:letter"},
		{"i7-14700K", "i:letter K:letter"},
		{"café", "café:word"},
		{"café", "café:word"},
		{"ＰＤＦ", "ＰＤＦ:acronym"},
		{"㎏", "㎏:word"},
		{"™", "™:acronym"},
		{"Python's", "Python's:word"},
		{"'quoted'", "quoted:word"},
		{"snake_case_name", "snake:word case:word name:word"},
		{"ΔT", "ΔT:acronym"},
		{"α線", "α:letter"},
		{"E=mc²", "E:letter mc:word"},
		{"日本語だけ", ""},
	}
	c := newConverter(t, Options{})
	for _, tt := range tests {
		var got []string
		for _, rw := range convert(t, c, tt.in).Rewrites {
			got = append(got, rw.Source+":"+string(rw.Class))
		}
		if s := strings.Join(got, " "); s != tt.want {
			t.Errorf("pieces of %q = %q, want %q", tt.in, s, tt.want)
		}
	}
}

func TestChains(t *testing.T) {
	tests := []struct {
		in, want string
		method   Method
	}{
		{"update", kanaOf("update"), Model},
		{"python", kanaOf("python"), Model},
		{"cafe", kanaOf("cafe"), Model},
		{"café", kanaOf("cafe"), Model},
		{"Python's", kanaOf("pythons"), Model},
		{"mp", "エムピー", Spell},
		{"npm", "エヌピーエム", Spell},
		{"pkg", "ピーケージー", Spell},
		{"cmd", "シーエムディー", Spell},
		{"src", "エスアールシー", Spell},
		{"API", "エーピーアイ", Spell},
		{"x", "エックス", Spell},
		{"λόγος", "ラムダオミクロンガンマオミクロンシグマ", Spell},
		{"ə", "", Drop},
		{strings.Repeat("ab", 17), strings.Repeat("エービー", 17), Spell},
	}
	c := newConverter(t, Options{Model: kanaModel{}})
	for _, tt := range tests {
		r := convert(t, c, tt.in)
		if r.Text != tt.want || len(r.Rewrites) != 1 || r.Rewrites[0].Method != tt.method {
			t.Errorf("Convert(%q) = %q %+v, want %q by %s", tt.in, r.Text, r.Rewrites, tt.want, tt.method)
		}
	}
}

type judgeFunc func(string) bool

func (f judgeFunc) Pronounceable(w string) bool { return f(w) }

func TestJudgeSeesFoldedWord(t *testing.T) {
	var seen []string
	c := newConverter(t, Options{Model: kanaModel{}, Judge: judgeFunc(func(w string) bool {
		seen = append(seen, w)
		return false
	})})
	r := convert(t, c, "Café")
	if r.Text != "シーエーエフイー" || strings.Join(seen, ",") != "cafe" {
		t.Errorf("got %q, judge saw %q", r.Text, seen)
	}
}

func TestNoModelSpells(t *testing.T) {
	c := newConverter(t, Options{})
	r := convert(t, c, "updateは")
	if r.Text != "ユーピーディーエーティーイーは" {
		t.Errorf("got %q", r.Text)
	}
}

func TestHostileModel(t *testing.T) {
	c := newConverter(t, Options{Model: hostileModel{}})
	in := "update python cafe hello world GitHub naïve Straße zebra quartz"
	r := convert(t, c, in)
	if script.HasLetter(r.Text) || !utf8.ValidString(r.Text) {
		t.Errorf("Convert(%q) = %q", in, r.Text)
	}
	for _, rw := range r.Rewrites {
		if rw.Method == Model && (rw.Result == "" || script.HasLetter(rw.Result) || !utf8.ValidString(rw.Result)) {
			t.Errorf("hostile output accepted: %+v", rw)
		}
	}
}

type cancelModel struct{ cancel context.CancelFunc }

func (m cancelModel) ConvertContext(ctx context.Context, s string) (string, error) {
	m.cancel()
	return "", ctx.Err()
}

func TestConvertCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := newConverter(t, Options{Model: cancelModel{cancel}})
	if _, err := c.Convert(ctx, "日本語の update です"); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if _, err := c.Convert(ctx, "日本語だけ"); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v on canceled ctx, want context.Canceled", err)
	}
}

func TestDictionaries(t *testing.T) {
	user := mustDict(t, `
name = "user"
[literal]
GitHub = "ギッハブ"
"Python" = "パイソン"

[[regex]]
id = "san"
pattern = '([A-Za-z]+)さん'
replace = "$1さん"

[[regex]]
id = "semver"
pattern = 'v(\d+)\.(\d+)'
replace = "バージョン$1.$2"
`)
	c := newConverter(t, Options{Dictionaries: append(builtin(t), user), Model: kanaModel{}})
	tests := []struct{ in, want string }{
		{"GitHubで", "ギッハブで"},
		{"GitHub Actionsで", "ギットハブアクションズで"},
		{"main.pyを", kanaOf("main") + "ドットパイを"},
		{"Python'sの", kanaOf("pythons") + "の"},
		{"v1.2から", "バージョン1.2から"},
		{"Bobさん", kanaOf("bob") + "さん"},
		{"2.4GHz帯", "2.4ギガヘルツ帯"},
		{"10gの塩", "10グラムの塩"},
		{"k8sで", "ケーエイツで"},
	}
	for _, tt := range tests {
		r := convert(t, c, tt.in)
		if r.Text != tt.want {
			t.Errorf("Convert(%q) = %q, want %q", tt.in, r.Text, tt.want)
		}
	}

	r := convert(t, c, "Bobさん")
	if len(r.Rewrites) != 1 {
		t.Fatalf("rewrites = %+v", r.Rewrites)
	}
	rw := r.Rewrites[0]
	want := dict.RuleRef{Dict: "user", Key: "san", Regex: true}
	if rw.Method != Dictionary || rw.Rule == nil || *rw.Rule != want {
		t.Errorf("rewrite = %+v, want rule %v", rw, want)
	}
	if len(rw.Inner) != 1 || rw.Inner[0].Source != "Bob" || rw.Inner[0].Start != 0 || rw.Inner[0].Method != Model {
		t.Errorf("inner = %+v", rw.Inner)
	}

	in := "砂糖を50gに"
	r = convert(t, c, in)
	if len(r.Rewrites) != 1 || r.Rewrites[0].Source != "g" || r.Rewrites[0].Result != "グラム" || in[r.Rewrites[0].Start:r.Rewrites[0].End] != "g" {
		t.Errorf("unit rewrite = %+v, want only g → グラム", r.Rewrites)
	}
}

func TestInvalidUTF8DoesNotFuse(t *testing.T) {
	c := newConverter(t, Options{})
	for _, in := range []string{"\xc3ə\xa9", "\xe3\x82ə\xa2", "\xc3əəa\xa9"} {
		r := convert(t, c, in)
		if script.HasLetter(r.Text) {
			t.Errorf("Convert(%q) = %q has a letter", in, r.Text)
		}
		checkRewrites(t, in, r)
	}
}

// checkRewrites asserts that the rewrites are ordered, non-overlapping,
// faithful to the input, and reproduce the output.
func checkRewrites(t *testing.T, in string, r Result) {
	t.Helper()
	var b strings.Builder
	prev := 0
	for _, rw := range r.Rewrites {
		if rw.Start < prev || rw.End <= rw.Start || rw.End > len(in) {
			t.Fatalf("Convert(%q): rewrite [%d,%d) after %d", in, rw.Start, rw.End, prev)
		}
		if rw.Source != in[rw.Start:rw.End] {
			t.Fatalf("Convert(%q): Source %q, input has %q", in, rw.Source, in[rw.Start:rw.End])
		}
		for _, x := range rw.Inner {
			if x.Start < rw.Start || x.End > rw.End || x.Source != in[x.Start:x.End] {
				t.Fatalf("Convert(%q): inner %+v outside %+v", in, x, rw)
			}
		}
		b.WriteString(in[prev:rw.Start])
		b.WriteString(rw.Result)
		prev = rw.End
	}
	b.WriteString(in[prev:])
	if b.String() != r.Text {
		t.Fatalf("Convert(%q): rewrites give %q, Text is %q", in, b.String(), r.Text)
	}
}

func FuzzConvert(f *testing.F) {
	for _, line := range corpusLines(f) {
		f.Add(line)
	}
	for _, s := range []string{
		"", "日本語", "\xff\xfe", "\xc3ə\xa9", "é", "́a", "a'b'", "'", "O'Reilly",
		"ＧｉｔＨｕｂ", "㎏㎡㎑", "90℃", "1kΩ", "E=mc²", "ǅungla", "İstanbul", "ﬁﬂ", "𝙈𝙮",
		"k8s 10g 2.4GHz 5km/h km/h", "Go1.18", "Google", "main.py", "GitHub Actions",
	} {
		f.Add(s)
	}
	dicts := builtin(f)
	converters := []*Converter{
		newConverter(f, Options{Dictionaries: dicts, Model: kanaModel{}}),
		newConverter(f, Options{Dictionaries: dicts, Model: hostileModel{}}),
	}
	f.Fuzz(func(t *testing.T, in string) {
		for i, c := range converters {
			r := convert(t, c, in)
			if script.HasLetter(r.Text) {
				t.Fatalf("converter %d: Convert(%q) = %q has a letter", i, in, r.Text)
			}
			if !script.HasLetter(in) && r.Text != in {
				t.Fatalf("converter %d: Convert(%q) = %q, want the input unchanged", i, in, r.Text)
			}
			if again := convert(t, c, r.Text); again.Text != r.Text {
				t.Fatalf("converter %d: Convert(%q) = %q, then %q", i, in, r.Text, again.Text)
			}
			checkRewrites(t, in, r)
		}
	})
}

func ExampleConverter_Convert() {
	ds, _ := dict.Builtin()
	c, _ := New(Options{Dictionaries: ds})
	r, _ := c.Convert(context.Background(), "JSONをGitHubに置き、2.4GHzで")
	fmt.Println(r.Text)
	// Output: ジェイソンをギットハブに置き、2.4ギガヘルツで
}
