package dict

import (
	"cmp"
	"slices"
	"strings"

	"github.com/tenkoh/go-yomikae/internal/script"
)

// Match is a dictionary entry found in the input.
type Match struct {
	Start, End int // byte range in the input
	Rule       RuleRef

	reading string
	tmpl    []part // nil for a literal
	groups  []int  // capture byte ranges in the input, -1 when unset
}

// Expand returns the reading of m. For a regex entry it calls convert on the
// byte range of each capture the replacement uses, once per group, and puts
// the result in its place. convert must return text without a Latin or Greek
// letter; invalid UTF-8 in it is replaced so that captures cannot fuse into a
// new rune.
func (m Match) Expand(convert func(start, end int) string) string {
	if m.tmpl == nil {
		return m.reading
	}
	var b strings.Builder
	done := map[int]string{}
	for _, p := range m.tmpl {
		if p.group < 0 {
			b.WriteString(p.text)
			continue
		}
		s, ok := done[p.group]
		if !ok {
			if start := m.groups[2*p.group]; start >= 0 {
				s = strings.ToValidUTF8(convert(start, m.groups[2*p.group+1]), "�")
			}
			done[p.group] = s
		}
		b.WriteString(s)
	}
	return b.String()
}

type candidate struct {
	m     Match
	prio  int // index of the dictionary
	order int // definition order within the dictionary
}

// Find returns the matches of dicts in s, ordered and non-overlapping. Among
// overlapping candidates it takes the leftmost, then the longest, then the
// one from the later dictionary, then the one defined first. Literals count
// as defined before regexes.
func Find(s string, dicts []*Dictionary) []Match {
	if len(dicts) == 0 {
		return nil
	}
	plain := newView(s, false)
	var folded *view
	var bd *bounds
	var cands []candidate
	for prio, d := range dicts {
		if len(d.literals) > 0 {
			v := plain
			if !d.caseSensitive {
				if folded == nil {
					folded = newView(s, true)
				}
				v = folded
			}
			if bd == nil {
				bd = newBounds(s)
			}
			for a := 0; a+1 < len(v.vstart); a++ {
				n := d.trie
				for p := v.vstart[a]; p < len(v.text); p++ {
					if n = n.next[v.text[p]]; n == nil {
						break
					}
					if n.lit < 0 {
						continue
					}
					b, ok := v.boundary(p + 1)
					lit := d.literals[n.lit]
					if !ok || !bd.allowed(a, b, lit.firstLetter, lit.lastLetter) {
						continue
					}
					cands = append(cands, candidate{m: Match{
						Start:   v.ostart[a],
						End:     v.ostart[b],
						Rule:    RuleRef{Dict: d.name, Key: lit.key},
						reading: lit.reading,
					}, prio: prio})
				}
			}
		}
		for i, re := range d.regexes {
		next:
			for _, loc := range re.re.FindAllStringSubmatchIndex(plain.text, -1) {
				groups := make([]int, len(loc))
				for j, p := range loc {
					if p < 0 {
						groups[j] = -1
						continue
					}
					ri, ok := plain.boundary(p)
					if !ok {
						continue next
					}
					groups[j] = plain.ostart[ri]
				}
				start, end := groups[0], groups[1]
				if !script.HasLetter(s[start:end]) {
					continue
				}
				cands = append(cands, candidate{m: Match{
					Start:  start,
					End:    end,
					Rule:   RuleRef{Dict: d.name, Key: re.id, Regex: true},
					tmpl:   re.tmpl,
					groups: groups,
				}, prio: prio, order: 1 + i})
			}
		}
	}

	slices.SortFunc(cands, func(x, y candidate) int {
		return cmp.Or(
			cmp.Compare(x.m.Start, y.m.Start),
			cmp.Compare(y.m.End, x.m.End),
			cmp.Compare(y.prio, x.prio),
			cmp.Compare(x.order, y.order),
		)
	})
	var out []Match
	end := 0
	for _, c := range cands {
		if c.m.Start >= end {
			out = append(out, c.m)
			end = c.m.End
		}
	}
	return out
}
