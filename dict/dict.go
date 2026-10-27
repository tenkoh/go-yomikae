// Package dict loads reading dictionaries and finds their entries in text.
//
// A dictionary is a TOML file:
//
//	name = "tech"
//	license = "MIT"
//	case_sensitive = false # the default
//
//	[literal]
//	GitHub = "ギットハブ"
//
//	[[regex]]
//	id = "semver"
//	pattern = 'v(\d+)\.(\d+)'
//	replace = "バージョン$1.$2"
//
// Keys and patterns match against the input with NFKC applied rune by rune,
// so ＧｉｔＨｕｂ matches GitHub, ㎏ is kg and ℃ is °C. A case-insensitive
// dictionary also lowercases literal keys and the input, and matches its
// patterns with the (?i) flag. Readings must not contain a Latin or Greek
// letter; captures used in a replacement are converted by the caller.
package dict

import (
	"bytes"
	"embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
	"github.com/tenkoh/go-yomikae/internal/script"
)

// Dictionary is a loaded dictionary. It is immutable and safe for concurrent
// use by multiple goroutines.
type Dictionary struct {
	name          string
	caseSensitive bool
	literals      []literal
	trie          *node
	regexes       []regex
}

type literal struct {
	key, reading            string
	firstLetter, lastLetter bool
}

// node is a trie over the bytes of normalized literal keys.
type node struct {
	next map[byte]*node
	lit  int // index into Dictionary.literals, or -1
}

type regex struct {
	id   string
	re   *regexp.Regexp
	tmpl []part
}

// part is a piece of a replacement: literal text, or a capture group.
type part struct {
	text  string
	group int // -1 for literal text
}

// RuleRef names the dictionary entry behind a match.
type RuleRef struct {
	Dict  string // the dictionary's name
	Key   string // the literal key, or the regex id
	Regex bool
}

func (r RuleRef) String() string {
	if r.Regex {
		return r.Dict + ":regex:" + r.Key
	}
	return r.Dict + ":" + r.Key
}

// Name returns the dictionary's name, from its name field or else its file
// name without the extension.
func (d *Dictionary) Name() string { return d.name }

type rawDict struct {
	Name          string            `toml:"name"`
	License       string            `toml:"license"`
	CaseSensitive bool              `toml:"case_sensitive"`
	Literal       map[string]string `toml:"literal"`
	Regex         []struct {
		ID      string  `toml:"id"`
		Pattern string  `toml:"pattern"`
		Replace *string `toml:"replace"`
	} `toml:"regex"`
}

// LoadFile reads the dictionary in the named file.
func LoadFile(name string) (*Dictionary, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, fmt.Errorf("dict: %w", err)
	}
	defer f.Close()
	return Load(name, f)
}

// Load reads a dictionary from r. file names the source in errors, and names
// the dictionary when it has no name field.
func Load(file string, r io.Reader) (*Dictionary, error) {
	d, err := load(file, r)
	if err != nil {
		return nil, fmt.Errorf("dict: %s: %w", file, err)
	}
	return d, nil
}

func load(file string, r io.Reader) (*Dictionary, error) {
	var raw rawDict
	md, err := toml.NewDecoder(r).Decode(&raw)
	if err != nil {
		return nil, err
	}
	if un := md.Undecoded(); len(un) > 0 {
		return nil, fmt.Errorf("unknown key %q", un[0].String())
	}
	d := &Dictionary{
		name:          raw.Name,
		caseSensitive: raw.CaseSensitive,
		trie:          &node{lit: -1},
	}
	if d.name == "" {
		d.name = strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	}

	// Sorted so that the trie, and so every error, is deterministic.
	keys := make([]string, 0, len(raw.Literal))
	for k := range raw.Literal {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	owner := make(map[string]string, len(keys))
	for _, key := range keys {
		reading := raw.Literal[key]
		if !script.HasLetter(key) {
			return nil, fmt.Errorf("literal %q: key has no Latin or Greek letter", key)
		}
		if script.HasLetter(reading) {
			return nil, fmt.Errorf("literal %q: reading %q has a Latin or Greek letter", key, reading)
		}
		norm := normalize(key, !d.caseSensitive)
		if prev, ok := owner[norm]; ok {
			return nil, fmt.Errorf("literal %q: key is the same as %q after normalization", key, prev)
		}
		owner[norm] = key
		first, _ := utf8.DecodeRuneInString(key)
		last, _ := utf8.DecodeLastRuneInString(key)
		d.trie.insert(norm, len(d.literals))
		d.literals = append(d.literals, literal{
			key:         key,
			reading:     reading,
			firstLetter: script.IsLetter(first),
			lastLetter:  script.IsLetter(last) || script.IsMark(last),
		})
	}

	ids := make(map[string]bool, len(raw.Regex))
	for i, e := range raw.Regex {
		if e.ID == "" {
			return nil, fmt.Errorf("regex #%d: missing id", i+1)
		}
		if ids[e.ID] {
			return nil, fmt.Errorf("regex %q: duplicate id", e.ID)
		}
		ids[e.ID] = true
		if e.Pattern == "" {
			return nil, fmt.Errorf("regex %q: missing pattern", e.ID)
		}
		if e.Replace == nil {
			return nil, fmt.Errorf("regex %q: missing replace", e.ID)
		}
		pat := e.Pattern
		if !d.caseSensitive {
			pat = "(?i)" + pat
		}
		re, err := regexp.Compile(pat)
		if err != nil {
			return nil, fmt.Errorf("regex %q: %w", e.ID, err)
		}
		tmpl, err := parseTemplate(*e.Replace, re)
		if err != nil {
			return nil, fmt.Errorf("regex %q: %w", e.ID, err)
		}
		d.regexes = append(d.regexes, regex{id: e.ID, re: re, tmpl: tmpl})
	}
	return d, nil
}

func (n *node) insert(key string, lit int) {
	for i := 0; i < len(key); i++ {
		next := n.next[key[i]]
		if next == nil {
			if n.next == nil {
				n.next = make(map[byte]*node)
			}
			next = &node{lit: -1}
			n.next[key[i]] = next
		}
		n = next
	}
	n.lit = lit
}

// parseTemplate splits a replacement into literal text and group references,
// with the syntax of regexp.Regexp.Expand: $1, ${1}, $name, ${name} and $$.
// Unlike Expand, it rejects a reference to a group the pattern lacks, which
// catches $1x written for ${1}x.
func parseTemplate(s string, re *regexp.Regexp) ([]part, error) {
	var parts []part
	var lit strings.Builder
	for len(s) > 0 {
		i := strings.IndexByte(s, '$')
		if i < 0 {
			lit.WriteString(s)
			break
		}
		lit.WriteString(s[:i])
		s = s[i:]
		if strings.HasPrefix(s, "$$") {
			lit.WriteByte('$')
			s = s[2:]
			continue
		}
		name, rest, ok := groupName(s)
		if !ok {
			lit.WriteByte('$')
			s = s[1:]
			continue
		}
		g := groupIndex(re, name)
		if g < 0 {
			return nil, fmt.Errorf("replace refers to group %q, which the pattern does not have", name)
		}
		if lit.Len() > 0 {
			parts = append(parts, part{text: lit.String(), group: -1})
			lit.Reset()
		}
		parts = append(parts, part{group: g})
		s = rest
	}
	if lit.Len() > 0 {
		parts = append(parts, part{text: lit.String(), group: -1})
	}
	for _, p := range parts {
		if p.group < 0 && script.HasLetter(p.text) {
			return nil, fmt.Errorf("replace has a Latin or Greek letter outside group references: %q", p.text)
		}
	}
	return parts, nil
}

// groupName parses the reference at the start of s, which begins with $.
func groupName(s string) (name, rest string, ok bool) {
	s = s[1:]
	brace := strings.HasPrefix(s, "{")
	if brace {
		s = s[1:]
	}
	i := 0
	for i < len(s) && (s[i] == '_' || '0' <= s[i] && s[i] <= '9' || 'a' <= s[i]|0x20 && s[i]|0x20 <= 'z') {
		i++
	}
	if i == 0 {
		return "", "", false
	}
	name, rest = s[:i], s[i:]
	if brace {
		if !strings.HasPrefix(rest, "}") {
			return "", "", false
		}
		rest = rest[1:]
	}
	return name, rest, true
}

func groupIndex(re *regexp.Regexp, name string) int {
	n := 0
	for _, c := range []byte(name) {
		if c < '0' || c > '9' {
			return re.SubexpIndex(name)
		}
		n = n*10 + int(c-'0')
		if n > re.NumSubexp() {
			return -1
		}
	}
	return n
}

//go:embed builtin/*.toml
var builtinFS embed.FS

// builtinFiles lists the builtin dictionaries, lowest priority first.
var builtinFiles = []string{"tech.toml", "units.toml"}

var loadBuiltin = sync.OnceValues(func() ([]*Dictionary, error) {
	var ds []*Dictionary
	for _, name := range builtinFiles {
		b, err := builtinFS.ReadFile("builtin/" + name)
		if err != nil {
			return nil, err
		}
		d, err := Load("builtin/"+name, bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		ds = append(ds, d)
	}
	return ds, nil
})

// Builtin returns the dictionaries embedded in go-yomikae, lowest priority
// first: tech (software terms) and units (units of measure after a number).
// Load user dictionaries after them so that user entries win ties.
func Builtin() ([]*Dictionary, error) {
	ds, err := loadBuiltin()
	return slices.Clone(ds), err
}
