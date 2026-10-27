// Package yomikae rewrites the Latin and Greek letters in Japanese text as
// katakana, for Japanese TTS models that garble text mixed with English.
//
// Conversion runs in four stages: dictionary matching, token extraction,
// splitting tokens into classified pieces, and reading each piece through a
// fallback chain that always succeeds. The output never contains a Latin or
// Greek letter, and input without one is returned byte for byte.
package yomikae

import (
	"cmp"
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/tenkoh/go-yomikae/dict"
	"github.com/tenkoh/go-yomikae/internal/script"
)

// Transliterator reads a lowercase ASCII word as katakana. *e2k.C2K from
// github.com/tenkoh/e2k-go satisfies it.
type Transliterator interface {
	ConvertContext(ctx context.Context, s string) (string, error)
}

// Judge reports whether a lowercase ASCII word reads as a word rather than
// letter by letter. *ngram.Model satisfies it.
type Judge interface {
	Pronounceable(word string) bool
}

// Options configures New.
type Options struct {
	// Dictionaries are matched before anything else. Later dictionaries win
	// ties between matches of the same span.
	Dictionaries []*dict.Dictionary
	// Model reads words. Nil reads every piece letter by letter.
	Model Transliterator
	// Judge decides which words go to Model. Nil uses a heuristic: a word is
	// pronounceable if it has a vowel (y counts) and no run of four
	// consonants.
	Judge Judge
}

// Converter converts text. It is safe for concurrent use by multiple
// goroutines if its Model and Judge are.
type Converter struct {
	dicts []*dict.Dictionary
	model Transliterator
	judge Judge
}

// New returns a Converter for opts.
func New(opts Options) (*Converter, error) {
	for _, d := range opts.Dictionaries {
		if d == nil {
			return nil, errors.New("yomikae: nil dictionary")
		}
	}
	c := &Converter{dicts: opts.Dictionaries, model: opts.Model, judge: opts.Judge}
	if c.judge == nil {
		c.judge = heuristic{}
	}
	return c, nil
}

// Method is how a Rewrite produced its result.
type Method string

const (
	Dictionary Method = "dictionary" // a dictionary entry
	Model      Method = "model"      // Options.Model
	Spell      Method = "spell"      // letter by letter
	Drop       Method = "drop"       // no reading; the letters are removed
)

// Class is the class of a piece.
type Class string

const (
	Word    Class = "word"    // two or more letters, not all upper case
	Acronym Class = "acronym" // two or more letters, all upper case
	Letter  Class = "letter"  // one letter
)

// Result is the outcome of Convert.
type Result struct {
	Text     string
	Rewrites []Rewrite // ordered by Start, non-overlapping
}

// Rewrite is one replaced range of the input. Applying every Rewrite of a
// Result to the input yields Result.Text.
type Rewrite struct {
	Start, End int    // byte range in the input
	Source     string // the input in [Start, End)
	Result     string
	Method     Method
	Class      Class         // empty for a dictionary match
	Rule       *dict.RuleRef // the dictionary entry, for a dictionary match
	// Inner lists the conversions of the captures that a regex entry's
	// replacement uses, by group. Their ranges lie within the match.
	Inner []Rewrite
}

// Convert returns s with every Latin and Greek letter replaced. It returns an
// error only when ctx is done.
func (c *Converter) Convert(ctx context.Context, s string) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if !script.HasLetter(s) {
		return Result{Text: s}, nil
	}
	var rws []Rewrite
	prev := 0
	for _, m := range dict.Find(s, c.dicts) {
		r, err := c.convertRange(ctx, s, prev, m.Start)
		if err != nil {
			return Result{}, err
		}
		rws = append(rws, r...)

		var inner []Rewrite
		reading := m.Expand(func(start, end int) string {
			r, e := c.convertRange(ctx, s, start, end)
			err = cmp.Or(err, e)
			text := apply(s, start, end, r)
			inner = append(inner, r...)
			return text
		})
		if err != nil {
			return Result{}, err
		}
		// A regex keeps guard text such as the digits before a unit; leave it
		// out of the rewrite so the trace shows only what changed.
		pre, suf := commonEnds(s[m.Start:m.End], reading)
		rws = append(rws, Rewrite{
			Start:  m.Start + pre,
			End:    m.End - suf,
			Source: s[m.Start+pre : m.End-suf],
			Result: reading[pre : len(reading)-suf],
			Method: Dictionary,
			Rule:   &m.Rule,
			Inner:  inner,
		})
		prev = m.End
	}
	r, err := c.convertRange(ctx, s, prev, len(s))
	if err != nil {
		return Result{}, err
	}
	rws = append(rws, r...)
	return Result{Text: apply(s, 0, len(s), rws), Rewrites: rws}, nil
}

// convertRange reads the pieces in s[start:end], outside any dictionary
// match.
func (c *Converter) convertRange(ctx context.Context, s string, start, end int) ([]Rewrite, error) {
	var rws []Rewrite
	for _, p := range pieces(s, start, end) {
		src := s[p.start:p.end]
		out, method, err := c.read(ctx, p.class, src)
		if err != nil {
			return nil, err
		}
		rws = append(rws, Rewrite{
			Start:  p.start,
			End:    p.end,
			Source: src,
			Result: out,
			Method: method,
			Class:  p.class,
		})
	}
	return rws, nil
}

// apply returns s[start:end] with rws, which lie in that range, applied.
// Where an empty result would join a stray UTF-8 lead byte to a stray
// continuation byte, forming a rune that was not in the input, the result
// becomes a space instead.
func apply(s string, start, end int, rws []Rewrite) string {
	var b strings.Builder
	prev := start
	gap := func(to, last int) {
		g := s[prev:to]
		if g == "" {
			return
		}
		if last >= 0 && !utf8.RuneStart(g[0]) {
			// Results are valid UTF-8, so an invalid tail is input bytes
			// and the rewrite just written is empty.
			if r, size := utf8.DecodeLastRuneInString(b.String()); r == utf8.RuneError && size == 1 {
				rws[last].Result = " "
				b.WriteByte(' ')
			}
		}
		b.WriteString(g)
	}
	for i, rw := range rws {
		gap(rw.Start, i-1)
		b.WriteString(rw.Result)
		prev = rw.End
	}
	gap(end, len(rws)-1)
	return b.String()
}

// commonEnds returns the byte lengths of the longest rune-aligned prefix and
// suffix that a and b share, never overlapping in either string.
func commonEnds(a, b string) (pre, suf int) {
	for pre < len(a) && pre < len(b) {
		ra, n := utf8.DecodeRuneInString(a[pre:])
		rb, m := utf8.DecodeRuneInString(b[pre:])
		if ra != rb || n != m || ra == utf8.RuneError {
			break
		}
		pre += n
	}
	for suf < len(a)-pre && suf < len(b)-pre {
		ra, n := utf8.DecodeLastRuneInString(a[:len(a)-suf])
		rb, m := utf8.DecodeLastRuneInString(b[:len(b)-suf])
		if ra != rb || n != m || ra == utf8.RuneError || suf+n > len(a)-pre || suf+m > len(b)-pre {
			break
		}
		suf += n
	}
	return pre, suf
}
