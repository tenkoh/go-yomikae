package dict

import (
	"fmt"
	"strings"
	"testing"
)

func mustLoad(t *testing.T, file, src string) *Dictionary {
	t.Helper()
	d, err := Load(file, strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name, src string
		want      []string // substrings of the error
	}{
		{"syntax", `[literal`, []string{"bad.toml"}},
		{"duplicate key", "[literal]\nGo = \"ゴー\"\nGo = \"ゴ\"", []string{"bad.toml", "Go"}},
		{"unknown key", "case_sensitve = true", []string{"bad.toml", "case_sensitve"}},
		{"reading with letter", "[literal]\nGitHub = \"ギットHub\"", []string{"bad.toml", `"GitHub"`, "reading"}},
		{"reading with Greek", "[literal]\nalpha = \"α\"", []string{`"alpha"`, "reading"}},
		{"reading with fullwidth", "[literal]\nPDF = \"ＰＤＦ\"", []string{`"PDF"`, "reading"}},
		{"key without letter", "[literal]\n\"123\" = \"いちにさん\"", []string{`"123"`, "no Latin or Greek letter"}},
		{"case folding collision", "[literal]\nJSON = \"ジェイソン\"\njson = \"じぇいそん\"", []string{`"json"`, `"JSON"`}},
		{"nfkc collision", "case_sensitive = true\n[literal]\nPDF = \"ピーディーエフ\"\n\"ＰＤＦ\" = \"ぴーでぃーえふ\"", []string{"PDF", "ＰＤＦ"}},
		{"bad pattern", "[[regex]]\nid = \"r\"\npattern = '('\nreplace = \"\"", []string{"bad.toml", `"r"`, "missing closing )"}},
		{"missing group", "[[regex]]\nid = \"r\"\npattern = 'v(\\d+)'\nreplace = \"$2\"", []string{`"r"`, `"2"`}},
		{"missing named group", "[[regex]]\nid = \"r\"\npattern = 'v(?P<major>\\d+)'\nreplace = \"${minor}\"", []string{`"r"`, `"minor"`}},
		{"group run into text", "[[regex]]\nid = \"r\"\npattern = 'v(\\d+)'\nreplace = \"$1x\"", []string{`"r"`, `"1x"`}},
		{"replace with letter", "[[regex]]\nid = \"r\"\npattern = 'v(\\d+)'\nreplace = \"version$1\"", []string{`"r"`, "version"}},
		{"missing id", "[[regex]]\npattern = 'a'\nreplace = \"エー\"", []string{"regex #1", "id"}},
		{"duplicate id", "[[regex]]\nid = \"r\"\npattern = 'a'\nreplace = \"エー\"\n[[regex]]\nid = \"r\"\npattern = 'b'\nreplace = \"ビー\"", []string{`"r"`, "duplicate"}},
		{"missing replace", "[[regex]]\nid = \"r\"\npattern = 'a'", []string{`"r"`, "replace"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load("bad.toml", strings.NewReader(tt.src))
			if err == nil {
				t.Fatal("Load succeeded")
			}
			for _, w := range tt.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q does not mention %q", err, w)
				}
			}
		})
	}
}

func TestLoadAccepts(t *testing.T) {
	d := mustLoad(t, "dir/my-words.toml", `
license = "MIT"
[literal]
"C++" = "シープラスプラス"
deleted = ""
[[regex]]
id = "money"
pattern = '(\d+)USD'
replace = "$1ドル$$"
`)
	if d.Name() != "my-words" {
		t.Errorf("Name() = %q, want the file name", d.Name())
	}
	got := expandAll("5USD and C++ deleted", []*Dictionary{d})
	if want := "5ドル$ and シープラスプラス "; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// expandAll applies the matches of dicts to s, keeping captures as they are.
func expandAll(s string, dicts []*Dictionary) string {
	var b strings.Builder
	prev := 0
	for _, m := range Find(s, dicts) {
		b.WriteString(s[prev:m.Start])
		b.WriteString(m.Expand(func(start, end int) string { return s[start:end] }))
		prev = m.End
	}
	b.WriteString(s[prev:])
	return b.String()
}

// describe lists the matches of dicts in s as text@start:rule.
func describe(s string, dicts []*Dictionary) string {
	var out []string
	for _, m := range Find(s, dicts) {
		out = append(out, fmt.Sprintf("%s@%d:%s", s[m.Start:m.End], m.Start, m.Rule))
	}
	return strings.Join(out, " ")
}

func TestFind(t *testing.T) {
	base := mustLoad(t, "base.toml", `
name = "base"
[literal]
Go = "ゴー"
GitHub = "ギットハブ"
"GitHub Actions" = "ギットハブアクションズ"
".py" = "ドットパイ"
"Node.js" = "ノードジェイエス"
".js" = "ドットジェイエス"
kg = "キログラム"
"café" = "カフェ"
Python = "パイソン"
[[regex]]
id = "ver"
pattern = 'v\d+'
replace = "バージョン"
[[regex]]
id = "ver2"
pattern = 'v\d+'
replace = "ブイ"
[[regex]]
id = "digits"
pattern = '\d+'
replace = "数"
`)
	user := mustLoad(t, "user.toml", `
name = "user"
[literal]
GitHub = "ギッハブ"
`)
	units := mustLoad(t, "units.toml", `
name = "units"
case_sensitive = true
[literal]
MHz = "メガヘルツ"
`)
	dicts := []*Dictionary{base, user, units}
	tests := []struct{ in, want string }{
		// Boundaries apply only at a key edge that is a letter.
		{"Goで", "Go@0:base:Go"},
		{"Google", ""},
		{"Go1.18", "Go@0:base:Go"},
		{"main.py", ".py@4:base:.py"},
		{"main.python", "python@5:base:Python"},
		{"Python's", ""},
		{"café", ""},
		// Later dictionaries win ties; longer matches beat priority.
		{"GitHubに", "GitHub@0:user:GitHub"},
		{"GitHub Actionsで", "GitHub Actions@0:base:GitHub Actions"},
		// Leftmost wins over longer matches that start later.
		{"Node.jsで", "Node.js@0:base:Node.js"},
		// Regexes: definition order breaks ties; letter-free matches are dropped.
		{"v12", "v12@0:base:regex:ver"},
		{"123", ""},
		// Case folding and NFKC.
		{"GITHUBとｇｉｔｈｕｂ", "GITHUB@0:user:GitHub ｇｉｔｈｕｂ@9:user:GitHub"},
		{"5㎏", "㎏@1:base:kg"},
		{"MHzとmhz", "MHz@0:units:MHz"},
		{"㎒", "㎒@0:units:MHz"},
	}
	for _, tt := range tests {
		if got := describe(tt.in, dicts); got != tt.want {
			t.Errorf("Find(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFindAlignsToRunes(t *testing.T) {
	d := mustLoad(t, "t.toml", `
[literal]
g = "グ"
[[regex]]
id = "k"
pattern = 'k(g)'
replace = "ケー$1"
`)
	// ㎏ expands to kg; neither a match nor a capture may split it.
	if got := describe("㎏", []*Dictionary{d}); got != "" {
		t.Errorf("Find(㎏) = %q, want no match", got)
	}
}

func TestExpandCapturesOncePerGroup(t *testing.T) {
	d := mustLoad(t, "t.toml", `
[[regex]]
id = "twice"
pattern = '(\w+)!'
replace = "$1、${1}"
`)
	ms := Find("hey!", []*Dictionary{d})
	if len(ms) != 1 {
		t.Fatalf("matches = %v", ms)
	}
	calls := 0
	got := ms[0].Expand(func(start, end int) string {
		calls++
		return "ヘイ"
	})
	if got != "ヘイ、ヘイ" || calls != 1 {
		t.Errorf("Expand = %q after %d calls", got, calls)
	}
}

func TestBuiltin(t *testing.T) {
	ds, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range ds {
		names = append(names, d.Name())
	}
	if got := strings.Join(names, ","); got != "tech,units" {
		t.Errorf("builtin dictionaries = %s", got)
	}
}

func TestBuiltinReadings(t *testing.T) {
	ds, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ in, want string }{
		{"10gの塩", "10グラムの塩"},
		{"水1.5Lと500mL", "水1.5リットルと500ミリリットル"},
		{"2.4GHz帯", "2.4ギガヘルツ帯"},
		{"時速5km/h", "時速5キロメートル毎時"},
		{"90℃と100°Cと0 °C", "90度と100度と0度"},
		{"9.8m/s²", "9.8メートル毎秒毎秒"},
		{"8GBのメモリ", "8ギガバイトのメモリ"},
		{"1kΩは1000Ω", "1キロオームは1000オーム"},
		{"12g/dL未満", "12グラム毎デシリットル未満"},
		{"5㎏", "5キログラム"},
		{"１０ｇ", "１０グラム"},
		{"100°F", "華氏100度"},
		{"k8sとmp3とe2e", "ケーエイツとmp3とe2e"},
		{"5Gと4Kと7B", "5Gと4Kと7B"},
		{"gとmだけ", "gとmだけ"},
		{"JSONとYAMLとNASAとGIF", "ジェイソンとヤムルとナサとジフ"},
		{"Wi-FiとmacOSとiOS", "ワイファイとマックオーエスとアイオーエス"},
		{"main.pyとmain.goとREADME.md", "mainドットパイとmainドットゴーとリードミードットエムディー"},
		{"apiとurlとcli", "エーピーアイとユーアールエルとシーエルアイ"},
		{"Hello", "Hello"},
		{"Goで書く", "ゴーで書く"},
		{"foo.co.jp", "fooドットシーオードットジェーピー"},
		{"user_id", "user_アイディー"},
		{"getUserById", "getUserById"},
	}
	for _, tt := range tests {
		if got := expandAll(tt.in, ds); got != tt.want {
			t.Errorf("builtin(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
