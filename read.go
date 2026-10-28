package yomikae

import (
	"context"
	"unicode/utf8"

	"github.com/tenkoh/go-yomikae/internal/script"
)

// maxModelInput is the longest folded piece sent to the model. Its output
// stops at 16 symbols, so longer words come out cut short.
const maxModelInput = 32

// generator returns a reading of a piece. An empty result, or one with a
// letter or invalid UTF-8, passes the piece to the next generator.
type generator struct {
	method Method
	read   func(c *Converter, ctx context.Context, src string) (string, error)
}

var chains = map[Class][]generator{
	Word:    {{Model, (*Converter).transliterate}, {Spell, spell}},
	Acronym: {{Spell, spell}},
	Letter:  {{Spell, spell}},
}

// read returns the reading of src, a piece of class. It reports Drop when no
// generator gives a reading.
func (c *Converter) read(ctx context.Context, class Class, src string) (string, Method, error) {
	for _, g := range chains[class] {
		out, err := g.read(c, ctx, src)
		if err != nil {
			return "", "", err
		}
		if out != "" && utf8.ValidString(out) && !script.HasLetter(out) {
			return out, g.method, nil
		}
	}
	return "", Drop, nil
}

// transliterate reads src with the model if the judge says it is a word.
// Model failures other than ctx being done fall through to spelling.
func (c *Converter) transliterate(ctx context.Context, src string) (string, error) {
	if c.model == nil {
		return "", nil
	}
	w, ok := script.Fold(src)
	if !ok || len(w) > maxModelInput || !c.judge.Pronounceable(w) {
		return "", nil
	}
	out, err := c.model.ConvertContext(ctx, w)
	if err != nil {
		return "", ctx.Err()
	}
	return out, nil
}

func spell(_ *Converter, _ context.Context, src string) (string, error) {
	return script.Spell(src), nil
}

// heuristic is the Judge used without a model: a word is pronounceable if it
// has a vowel and no run of four consonants.
type heuristic struct{}

func (heuristic) Pronounceable(w string) bool {
	vowel, run := false, 0
	for i := 0; i < len(w); i++ {
		switch w[i] {
		case 'a', 'e', 'i', 'o', 'u', 'y':
			vowel, run = true, 0
		default:
			if run++; run >= 4 {
				return false
			}
		}
	}
	return vowel
}
