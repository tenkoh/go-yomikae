package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"io"
	"os"

	"github.com/tenkoh/go-yomikae"
)

func runConvert(ctx context.Context, e env, cfg *config, fs *flag.FlagSet, args []string) error {
	trace := fs.Bool("trace", false, "write one JSON object per rewrite to stderr")
	if err := cfg.parse(e, fs, args); err != nil {
		return err
	}
	c, err := cfg.converter(e)
	if err != nil {
		return err
	}
	out := bufio.NewWriter(e.stdout)
	enc := json.NewEncoder(e.stderr)
	enc.SetEscapeHTML(false)
	err = eachLine(e, fs.Args(), func(file string, n int, line string) error {
		r, err := c.Convert(ctx, line)
		if err != nil {
			return err
		}
		out.WriteString(r.Text)
		if file == "" {
			// Stdin may be interactive.
			if err := out.Flush(); err != nil {
				return err
			}
		}
		if *trace {
			for _, rw := range r.Rewrites {
				if err := writeTrace(enc, file, n, rw, false); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if ferr := out.Flush(); err == nil {
		err = ferr
	}
	return err
}

type traceRecord struct {
	File    string         `json:"file,omitempty"`
	Line    int            `json:"line"`
	Start   int            `json:"start"`
	End     int            `json:"end"`
	Source  string         `json:"source"`
	Result  string         `json:"result"`
	Method  yomikae.Method `json:"method"`
	Class   yomikae.Class  `json:"class,omitempty"`
	Rule    string         `json:"rule,omitempty"`
	Capture bool           `json:"capture,omitempty"`
}

// writeTrace writes rw, then the conversions of its captures. Byte ranges
// are relative to the line.
func writeTrace(enc *json.Encoder, file string, line int, rw yomikae.Rewrite, capture bool) error {
	rec := traceRecord{
		File:    file,
		Line:    line,
		Start:   rw.Start,
		End:     rw.End,
		Source:  rw.Source,
		Result:  rw.Result,
		Method:  rw.Method,
		Class:   rw.Class,
		Capture: capture,
	}
	if rw.Rule != nil {
		rec.Rule = rw.Rule.String()
	}
	if err := enc.Encode(rec); err != nil {
		return err
	}
	for _, x := range rw.Inner {
		if err := writeTrace(enc, file, line, x, true); err != nil {
			return err
		}
	}
	return nil
}

// eachLine calls fn for every line of files, or of stdin when there are none
// or a file is "-". Lines keep their terminators; file is "" for stdin and
// n counts from 1.
func eachLine(e env, files []string, fn func(file string, n int, line string) error) error {
	if len(files) == 0 {
		files = []string{"-"}
	}
	for _, name := range files {
		if name == "-" {
			if err := readLines(e.stdin, "", fn); err != nil {
				return err
			}
			continue
		}
		f, err := os.Open(name)
		if err != nil {
			return err
		}
		err = readLines(f, name, fn)
		f.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func readLines(r io.Reader, file string, fn func(file string, n int, line string) error) error {
	br := bufio.NewReader(r)
	for n := 1; ; n++ {
		line, err := br.ReadString('\n')
		if line != "" {
			if err := fn(file, n, line); err != nil {
				return err
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
