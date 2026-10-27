package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"slices"
	"strings"
	"text/tabwriter"
	"unicode"

	"github.com/tenkoh/go-yomikae"
	"github.com/tenkoh/go-yomikae/internal/script"
)

func runStats(ctx context.Context, e env, cfg *config, fs *flag.FlagSet, args []string) error {
	check := fs.Bool("check", false, "exit non-zero, listing the lines whose output still has a Latin or Greek letter")
	top := fs.Int("top", 5, "examples per row")
	if err := cfg.parse(e, fs, args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return errors.New("stats: no corpus files")
	}
	c, err := cfg.converter(e)
	if err != nil {
		return err
	}

	var lines, lettered int
	spans, spansMixed := newTally(), newTally()
	classes, methods := newTally(), newTally()
	var failed []string
	md := markdownFilter{}
	err = eachLine(e, fs.Args(), func(file string, n int, line string) error {
		if !md.keep(file, n, line) {
			return nil
		}
		lines++
		if !script.HasLetter(line) {
			return nil
		}
		lettered++
		for _, sp := range yomikae.Spans(line) {
			spans.add(string(sp.Kind), line[sp.Start:sp.End])
			if sp.Mixed {
				spansMixed.add(string(sp.Kind), "")
			}
		}
		r, err := c.Convert(ctx, line)
		if err != nil {
			return err
		}
		for _, rw := range r.Rewrites {
			class := string(rw.Class)
			if rw.Method == yomikae.Dictionary {
				class = "(dictionary)"
			}
			classes.add(class, rw.Source)
			methods.add(string(rw.Method), rw.Source+"→"+rw.Result)
		}
		if script.HasLetter(r.Text) {
			failed = append(failed, fmt.Sprintf("%s:%d: %s", file, n, strings.TrimRight(r.Text, "\r\n")))
		}
		return nil
	})
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(e.stdout, 0, 4, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintf(e.stdout, "lines %d, with letters %d (%s)\n\n", lines, lettered, share(lettered, lines))
	spans.print(w, "span kind", *top, spansMixed)
	fmt.Fprintln(e.stdout)
	classes.print(w, "piece class", *top, nil)
	fmt.Fprintln(e.stdout)
	methods.print(w, "method", *top, nil)

	if *check && len(failed) > 0 {
		fmt.Fprintln(e.stdout)
		for _, f := range failed {
			fmt.Fprintln(e.stdout, f)
		}
		return fmt.Errorf("stats: %d lines still have a Latin or Greek letter", len(failed))
	}
	return nil
}

// tally counts rows and, per row, how often each example occurs.
type tally struct {
	counts   map[string]int
	examples map[string]map[string]int
	total    int
}

func newTally() *tally {
	return &tally{counts: map[string]int{}, examples: map[string]map[string]int{}}
}

func (t *tally) add(row, example string) {
	t.counts[row]++
	t.total++
	if t.examples[row] == nil {
		t.examples[row] = map[string]int{}
	}
	t.examples[row][example]++
}

func (t *tally) print(w *tabwriter.Writer, title string, top int, mixed *tally) {
	rows := sortedKeys(t.counts)
	head := title + "\tcount\tshare\t"
	if mixed != nil {
		head += "mixed\t"
	}
	fmt.Fprintln(w, head+"  examples")
	for _, row := range rows {
		line := fmt.Sprintf("%s\t%d\t%s\t", row, t.counts[row], share(t.counts[row], t.total))
		if mixed != nil {
			line += fmt.Sprintf("%d\t", mixed.counts[row])
		}
		var ex []string
		for _, x := range sortedKeys(t.examples[row]) {
			if len(ex) == top {
				break
			}
			ex = append(ex, fmt.Sprintf("%s(%d)", x, t.examples[row][x]))
		}
		fmt.Fprintln(w, line+"  "+strings.Join(ex, " "))
	}
	total := fmt.Sprintf("total\t%d\t%s\t", t.total, share(t.total, t.total))
	if mixed != nil {
		total += fmt.Sprintf("%d\t", mixed.total)
	}
	fmt.Fprintln(w, total)
	w.Flush()
}

// sortedKeys returns the keys of m by descending count, then by key.
func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int {
		return cmp.Or(cmp.Compare(m[b], m[a]), strings.Compare(a, b))
	})
	return keys
}

func share(n, total int) string {
	if total == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", 100*float64(n)/float64(total))
}

// markdownFilter keeps the prose of Markdown files as the corpus survey in
// docs/design.md does: no front matter, no fenced code, only lines with
// Japanese. Other files keep every line.
type markdownFilter struct {
	frontMatter, fence bool
}

func (m *markdownFilter) keep(file string, n int, line string) bool {
	if !strings.HasSuffix(file, ".md") {
		return true
	}
	t := strings.TrimSpace(line)
	if n == 1 {
		m.frontMatter, m.fence = t == "---", false
		if m.frontMatter {
			return false
		}
	}
	switch {
	case m.frontMatter:
		m.frontMatter = t != "---"
		return false
	case strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~"):
		m.fence = !m.fence
		return false
	case m.fence:
		return false
	}
	return strings.IndexFunc(line, func(r rune) bool {
		return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana)
	}) >= 0
}
