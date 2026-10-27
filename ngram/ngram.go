// Package ngram judges whether a word is pronounceable with the NGram model of
// Patchethium/e2k (https://github.com/Patchethium/e2k): a linear SVM over the
// interpolated Kneser-Ney scores of character models of orders 1 to N.
package ngram

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Model is a loaded NGram model.
//
// A Model is immutable after Load and safe for concurrent use by multiple
// goroutines.
type Model struct {
	// models[i] holds the levels of the order i+1 model, lowest order first.
	// The levels below the top one are shared by all models.
	models    [][]*level
	vocab     float64
	weights   []float64
	bias      float64
	threshold float64
	valid     [128]bool
}

// level is one order k of a Kneser-Ney model, keyed by histories of length k-1.
type level struct {
	discount float64
	hists    map[string]*history
}

type history struct {
	total, types float64
	counts       map[byte]float64
}

type rawLevel struct {
	Discount float64        `json:"discount"`
	Counts   map[string]int `json:"counts"`
}

type rawModel struct {
	Orders    []int      `json:"orders"`
	Vocab     int        `json:"vocab"`
	Lower     []rawLevel `json:"lower"`
	Top       []rawLevel `json:"top"`
	Weights   []float64  `json:"weights"`
	Bias      *float64   `json:"bias"`
	Threshold *float64   `json:"threshold"`
}

// Load reads an NGram model in the JSON format of ngram.json, the single
// member of ngram.json.zip from the upstream package. The caller closes r.
func Load(r io.Reader) (*Model, error) {
	var raw rawModel
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, fmt.Errorf("ngram: %w", err)
	}
	m, err := newModel(&raw)
	if err != nil {
		return nil, fmt.Errorf("ngram: %w", err)
	}
	return m, nil
}

func newModel(raw *rawModel) (*Model, error) {
	n := len(raw.Orders)
	switch {
	case n == 0:
		return nil, errors.New("no orders")
	case raw.Vocab <= 0:
		return nil, fmt.Errorf("vocab is %d, want > 0", raw.Vocab)
	case raw.Bias == nil || raw.Threshold == nil:
		return nil, errors.New("missing bias or threshold")
	case len(raw.Top) != n:
		return nil, fmt.Errorf("%d top levels for %d orders", len(raw.Top), n)
	case len(raw.Weights) != n:
		return nil, fmt.Errorf("%d weights for %d orders", len(raw.Weights), n)
	case len(raw.Lower) != n-1:
		return nil, fmt.Errorf("%d lower levels for %d orders, want %d", len(raw.Lower), n, n-1)
	}
	for i, o := range raw.Orders {
		if o != i+1 {
			return nil, fmt.Errorf("orders[%d] is %d, want %d", i, o, i+1)
		}
	}

	lower := make([]*level, n-1)
	for i := range raw.Lower {
		lv, err := newLevel(&raw.Lower[i], i+1)
		if err != nil {
			return nil, fmt.Errorf("lower[%d]: %w", i, err)
		}
		lower[i] = lv
	}
	m := &Model{
		models:    make([][]*level, n),
		vocab:     float64(raw.Vocab),
		weights:   raw.Weights,
		bias:      *raw.Bias,
		threshold: *raw.Threshold,
	}
	for i := range raw.Top {
		top, err := newLevel(&raw.Top[i], i+1)
		if err != nil {
			return nil, fmt.Errorf("top[%d]: %w", i, err)
		}
		m.models[i] = append(lower[:i:i], top)
	}
	// The unigrams are the characters seen in training, and the end of word.
	for gram := range raw.Top[0].Counts {
		m.valid[gram[0]] = true
	}
	m.valid['$'] = false
	return m, nil
}

func newLevel(raw *rawLevel, order int) (*level, error) {
	if len(raw.Counts) == 0 {
		return nil, errors.New("no counts")
	}
	lv := &level{discount: raw.Discount, hists: make(map[string]*history)}
	for gram, c := range raw.Counts {
		if len(gram) != order {
			return nil, fmt.Errorf("gram %q has length %d, want %d", gram, len(gram), order)
		}
		for i := 0; i < len(gram); i++ {
			if gram[i] >= utf8.RuneSelf {
				return nil, fmt.Errorf("gram %q is not ASCII", gram)
			}
		}
		if c <= 0 {
			return nil, fmt.Errorf("gram %q has count %d, want > 0", gram, c)
		}
		h := lv.hists[gram[:order-1]]
		if h == nil {
			h = &history{counts: make(map[byte]float64)}
			lv.hists[gram[:order-1]] = h
		}
		h.total += float64(c)
		h.types++
		h.counts[gram[order-1]] = float64(c)
	}
	return lv, nil
}

// Score returns how pronounceable word is, pronounceable above the model's
// threshold. Input is lowercased and characters outside the model's alphabet
// are dropped. A phrase scores as its least pronounceable word. Score returns
// -Inf if no character is left.
func (m *Model) Score(word string) float64 {
	best := math.Inf(-1)
	seen := false
	buf := make([]byte, 0, len(word)+2)
	for _, part := range strings.FieldsFunc(word, isSpace) {
		buf = append(buf[:0], '^')
		for _, r := range part {
			r = unicode.ToLower(r)
			if r < utf8.RuneSelf && m.valid[r] {
				buf = append(buf, byte(r))
			}
		}
		if len(buf) == 1 {
			continue
		}
		buf = append(buf, '$')
		s := m.score(string(buf))
		if !seen || s < best {
			best, seen = s, true
		}
	}
	return best
}

// Pronounceable reports whether word scores above the model's threshold, so
// that it reads as a word rather than letter by letter.
func (m *Model) Pronounceable(word string) bool {
	return m.Score(word) > m.threshold
}

// isSpace matches Python's str.split, which also splits on the ASCII
// information separators.
func isSpace(r rune) bool {
	return unicode.IsSpace(r) || 0x1c <= r && r <= 0x1f
}

// score is the SVM decision value for s, a word wrapped in ^ and $.
func (m *Model) score(s string) float64 {
	sum := 0.0
	for i, levels := range m.models {
		sum += m.weights[i] * m.meanLogProb(levels, s)
	}
	return sum + m.bias
}

// meanLogProb is the mean log probability per character of s, including the
// end of word.
func (m *Model) meanLogProb(levels []*level, s string) float64 {
	n := len(levels)
	sum := 0.0
	for i := 1; i < len(s); i++ {
		sum += math.Log(m.prob(levels, s[i], s[max(0, i-n+1):i]))
	}
	return sum / float64(len(s)-1)
}

// prob is P(c | hist), interpolated down to a uniform distribution.
func (m *Model) prob(levels []*level, c byte, hist string) float64 {
	p := 1 / m.vocab
	for k := 1; k <= len(hist)+1; k++ {
		lv := levels[k-1]
		h, ok := lv.hists[hist[len(hist)-k+1:]]
		if !ok {
			continue
		}
		p = max(h.counts[c]-lv.discount, 0)/h.total + lv.discount*h.types/h.total*p
	}
	return p
}
