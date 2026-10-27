package ngram

import (
	"encoding/json"
	"math"
	"os"
	"strings"
	"sync"
	"testing"
)

type scoreCase struct {
	Word          string   `json:"word"`
	Score         *float64 `json:"score"` // nil for -Inf
	Pronounceable bool     `json:"pronounceable"`
}

func loadModel(t *testing.T, path string) *Model {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	m, err := Load(f)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func loadCases(t *testing.T, path string) []scoreCase {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cases []scoreCase
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	return cases
}

func checkScores(t *testing.T, m *Model, cases []scoreCase) {
	t.Helper()
	for _, c := range cases {
		got := m.Score(c.Word)
		if c.Score == nil {
			if !math.IsInf(got, -1) {
				t.Errorf("Score(%q) = %v, want -Inf", c.Word, got)
			}
		} else if want := *c.Score; math.Abs(got-want) > 1e-9*math.Abs(want) {
			t.Errorf("Score(%q) = %.17g, want %.17g", c.Word, got, want)
		}
		if got := m.Pronounceable(c.Word); got != c.Pronounceable {
			t.Errorf("Pronounceable(%q) = %v, want %v", c.Word, got, c.Pronounceable)
		}
	}
}

// The expected scores come from the upstream NGram class, see testdata/gen.py.
func TestScoreTiny(t *testing.T) {
	m := loadModel(t, "testdata/tiny.json")
	checkScores(t, m, loadCases(t, "testdata/tiny-scores.json"))
}

func TestConcurrentScore(t *testing.T) {
	m := loadModel(t, "testdata/tiny.json")
	want := m.Score("ba ab")
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				if got := m.Score("ba ab"); got != want {
					t.Errorf("Score = %v, want %v", got, want)
					return
				}
			}
		})
	}
	wg.Wait()
}

func TestLoadErrors(t *testing.T) {
	base, err := os.ReadFile("testdata/tiny.json")
	if err != nil {
		t.Fatal(err)
	}
	edit := func(f func(m map[string]any)) string {
		var m map[string]any
		if err := json.Unmarshal(base, &m); err != nil {
			t.Fatal(err)
		}
		f(m)
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	level := func(m map[string]any, key string, i int) map[string]any {
		return m[key].([]any)[i].(map[string]any)
	}
	tests := map[string]string{
		"not json":         "{",
		"no orders":        edit(func(m map[string]any) { m["orders"] = []any{} }),
		"orders gap":       edit(func(m map[string]any) { m["orders"] = []any{1, 2, 4} }),
		"zero vocab":       edit(func(m map[string]any) { m["vocab"] = 0 }),
		"missing bias":     edit(func(m map[string]any) { delete(m, "bias") }),
		"missing thresh":   edit(func(m map[string]any) { delete(m, "threshold") }),
		"short weights":    edit(func(m map[string]any) { m["weights"] = []any{0.1, 0.2} }),
		"short top":        edit(func(m map[string]any) { m["top"] = m["top"].([]any)[:2] }),
		"long lower":       edit(func(m map[string]any) { m["lower"] = append(m["lower"].([]any), level(m, "top", 2)) }),
		"wrong gram order": edit(func(m map[string]any) { level(m, "top", 1)["counts"].(map[string]any)["abc"] = 1 }),
		"zero count":       edit(func(m map[string]any) { level(m, "lower", 0)["counts"].(map[string]any)["a"] = 0 }),
		"non-ascii gram":   edit(func(m map[string]any) { level(m, "top", 0)["counts"].(map[string]any)["é"] = 1 }),
		"empty counts":     edit(func(m map[string]any) { level(m, "top", 2)["counts"] = map[string]any{} }),
	}
	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(strings.NewReader(in)); err == nil {
				t.Error("Load succeeded, want error")
			}
		})
	}
}
