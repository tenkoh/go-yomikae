// Command yomikae rewrites the Latin and Greek letters in Japanese text as
// katakana for Japanese TTS.
//
// Usage:
//
//	yomikae [convert] [flags] [file ...]  convert files or stdin line by line
//	yomikae stats [flags] file ...        show span and piece distributions
//	yomikae serve [flags]                 run an OpenAI-compatible TTS proxy
//
// Every flag shared by the subcommands has an environment variable; a flag
// beats its variable. Run a subcommand with -h for its flags.
package main

import (
	"compress/gzip"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/tenkoh/e2k-go"
	"github.com/tenkoh/go-yomikae"
	"github.com/tenkoh/go-yomikae/dict"
	"github.com/tenkoh/go-yomikae/ngram"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Getenv, os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

type env struct {
	getenv         func(string) string
	stdin          io.Reader
	stdout, stderr io.Writer
}

var commands = map[string]func(ctx context.Context, e env, cfg *config, fs *flag.FlagSet, args []string) error{
	"convert": runConvert,
	"stats":   runStats,
	"serve":   runServe,
}

// errUsage reports a usage error that the flag package has already printed.
var errUsage = errors.New("usage")

func run(ctx context.Context, args []string, getenv func(string) string, stdin io.Reader, stdout, stderr io.Writer) int {
	name := "convert"
	if len(args) > 0 && commands[args[0]] != nil {
		name, args = args[0], args[1:]
	}
	e := env{getenv: getenv, stdin: stdin, stdout: stdout, stderr: stderr}
	fs := flag.NewFlagSet("yomikae "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := &config{}
	cfg.register(fs)
	err := commands[name](ctx, e, cfg, fs, args)
	switch {
	case err == nil:
		return 0
	case errors.Is(err, flag.ErrHelp), errors.Is(err, errDone):
		return 0
	case errors.Is(err, errUsage):
		return 2
	}
	fmt.Fprintf(stderr, "yomikae: %v\n", err)
	return 1
}

// config holds the flags every subcommand shares.
type config struct {
	c2k, ngram   string
	dicts        dictList
	noBuiltin    bool
	missingModel string
	licenses     bool
}

type dictList []string

func (l *dictList) String() string     { return strings.Join(*l, ":") }
func (l *dictList) Set(v string) error { *l = append(*l, v); return nil }

// envVars maps each flag to its environment variable.
var envVars = []struct{ flag, env string }{
	{"c2k", "YOMIKAE_C2K_MODEL"},
	{"ngram", "YOMIKAE_NGRAM_MODEL"},
	{"dict", "YOMIKAE_DICTS"},
	{"no-builtin-dicts", "YOMIKAE_NO_BUILTIN_DICTS"},
	{"missing-model", "YOMIKAE_MISSING_MODEL"},
}

func (cfg *config) register(fs *flag.FlagSet) {
	fs.StringVar(&cfg.c2k, "c2k", "", "path of the C2K weights, model-c2k.safetensors (env YOMIKAE_C2K_MODEL; see docs/models.md)")
	fs.StringVar(&cfg.ngram, "ngram", "", "path of NGram data replacing the bundled one, or none for a vowel heuristic (env YOMIKAE_NGRAM_MODEL)")
	fs.Var(&cfg.dicts, "dict", "dictionary TOML file, repeatable; later files win ties (env YOMIKAE_DICTS, ':'-separated)")
	fs.BoolVar(&cfg.noBuiltin, "no-builtin-dicts", false, "do not load the builtin dictionaries (env YOMIKAE_NO_BUILTIN_DICTS)")
	fs.StringVar(&cfg.missingModel, "missing-model", "error", "without C2K weights: error to stop, spell to read letter by letter (env YOMIKAE_MISSING_MODEL)")
	fs.BoolVar(&cfg.licenses, "licenses", false, "print third-party notices and exit")
}

// parse parses args, then fills each flag not given from its environment
// variable. It returns errDone after printing the notices for -licenses.
func (cfg *config) parse(e env, fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return errUsage
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	for _, v := range envVars {
		val := e.getenv(v.env)
		if set[v.flag] || val == "" {
			continue
		}
		vals := []string{val}
		if v.flag == "dict" {
			vals = strings.Split(val, ":")
		}
		for _, x := range vals {
			if x == "" {
				continue
			}
			if err := fs.Set(v.flag, x); err != nil {
				return fmt.Errorf("%s=%q: %w", v.env, val, err)
			}
		}
	}
	if cfg.missingModel != "error" && cfg.missingModel != "spell" {
		return fmt.Errorf("-missing-model is %q, want error or spell", cfg.missingModel)
	}
	if cfg.licenses {
		fmt.Fprint(e.stdout, ngram.Notices())
		return errDone
	}
	return nil
}

// errDone ends a subcommand successfully before it does its work.
var errDone = errors.New("done")

// converter builds the Converter that cfg describes.
func (cfg *config) converter(e env) (*yomikae.Converter, error) {
	var opts yomikae.Options
	if !cfg.noBuiltin {
		ds, err := dict.Builtin()
		if err != nil {
			return nil, err
		}
		opts.Dictionaries = ds
	}
	for _, name := range cfg.dicts {
		d, err := dict.LoadFile(name)
		if err != nil {
			return nil, err
		}
		opts.Dictionaries = append(opts.Dictionaries, d)
	}

	switch cfg.ngram {
	case "none":
	case "":
		m, err := ngram.Builtin()
		if err != nil {
			return nil, err
		}
		opts.Judge = m
	default:
		m, err := loadNgram(cfg.ngram)
		if err != nil {
			return nil, err
		}
		opts.Judge = m
	}

	model, err := loadC2K(cfg.c2k)
	switch {
	case err == nil:
		opts.Model = model
	case cfg.missingModel == "spell":
		fmt.Fprintf(e.stderr, "yomikae: warning: %v; reading every word letter by letter\n", err)
	default:
		return nil, fmt.Errorf("%w; set -c2k or YOMIKAE_C2K_MODEL to the weights described in docs/models.md, or pass -missing-model=spell to read letter by letter", err)
	}
	return yomikae.New(opts)
}

func loadC2K(name string) (*e2k.C2K, error) {
	if name == "" {
		return nil, errors.New("no C2K model given")
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, fmt.Errorf("C2K model: %w", err)
	}
	defer f.Close()
	m, err := e2k.LoadC2K(f)
	if err != nil {
		return nil, fmt.Errorf("C2K model %s: %w", name, err)
	}
	return m, nil
}

// loadNgram reads ngram.json, or ngram.json.gz as bundled in ngram/data.
func loadNgram(name string) (*ngram.Model, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var r io.Reader = f
	if strings.HasSuffix(name, ".gz") {
		zr, err := gzip.NewReader(f)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		r = zr
	}
	m, err := ngram.Load(r)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return m, nil
}
