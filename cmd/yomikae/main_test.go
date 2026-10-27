package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tenkoh/go-yomikae/internal/script"
)

func noEnv(string) string { return "" }

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

type outcome struct {
	code           int
	stdout, stderr string
}

func runCLI(t *testing.T, getenv func(string) string, stdin string, args ...string) outcome {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), args, getenv, strings.NewReader(stdin), &stdout, &stderr)
	return outcome{code, stdout.String(), stderr.String()}
}

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMissingModel(t *testing.T) {
	o := runCLI(t, noEnv, "update\n")
	if o.code != 1 || o.stdout != "" {
		t.Fatalf("got %+v, want exit 1 and no output", o)
	}
	for _, w := range []string{"-c2k", "YOMIKAE_C2K_MODEL", "docs/models.md", "-missing-model=spell"} {
		if !strings.Contains(o.stderr, w) {
			t.Errorf("stderr %q does not mention %s", o.stderr, w)
		}
	}

	o = runCLI(t, noEnv, "", "-c2k", filepath.Join(t.TempDir(), "missing.safetensors"))
	if o.code != 1 || !strings.Contains(o.stderr, "missing.safetensors") {
		t.Errorf("unreadable model: got %+v", o)
	}

	o = runCLI(t, envOf(map[string]string{"YOMIKAE_MISSING_MODEL": "spell"}), "updateとJSON\n")
	if o.code != 0 || o.stdout != "ユーピーディーエーティーイーとジェイソン\n" || !strings.Contains(o.stderr, "warning") {
		t.Errorf("spell mode: got %+v", o)
	}

	o = runCLI(t, noEnv, "", "-missing-model", "maybe")
	if o.code != 1 || !strings.Contains(o.stderr, "maybe") {
		t.Errorf("bad -missing-model: got %+v", o)
	}
}

func TestFlagsBeatEnv(t *testing.T) {
	envDict := writeFile(t, "env.toml", "[literal]\nfoo = \"エンブ\"\n")
	flagDict := writeFile(t, "flag.toml", "[literal]\nbar = \"フラグ\"\n")
	env := envOf(map[string]string{
		"YOMIKAE_DICTS":         envDict,
		"YOMIKAE_MISSING_MODEL": "error",
	})

	o := runCLI(t, env, "foo bar\n", "-missing-model=spell")
	if o.code != 0 || o.stdout != "エンブ ビーエーアール\n" {
		t.Errorf("env dictionary: got %+v", o)
	}
	o = runCLI(t, env, "foo bar\n", "-missing-model=spell", "-dict", flagDict)
	if o.code != 0 || o.stdout != "エフオーオー フラグ\n" {
		t.Errorf("-dict replaces YOMIKAE_DICTS: got %+v", o)
	}
	o = runCLI(t, envOf(map[string]string{"YOMIKAE_DICTS": envDict + ":" + flagDict}), "foo bar\n", "-missing-model=spell")
	if o.code != 0 || o.stdout != "エンブ フラグ\n" {
		t.Errorf("':'-separated YOMIKAE_DICTS: got %+v", o)
	}
	o = runCLI(t, envOf(map[string]string{"YOMIKAE_NO_BUILTIN_DICTS": "1"}), "JSON\n", "-missing-model=spell")
	if o.code != 0 || o.stdout != "ジェーエスオーエヌ\n" {
		t.Errorf("YOMIKAE_NO_BUILTIN_DICTS: got %+v", o)
	}
	o = runCLI(t, envOf(map[string]string{"YOMIKAE_NO_BUILTIN_DICTS": "1"}), "JSON\n", "-missing-model=spell", "-no-builtin-dicts=false")
	if o.code != 0 || o.stdout != "ジェイソン\n" {
		t.Errorf("-no-builtin-dicts=false beats the env: got %+v", o)
	}
}

func TestLicenses(t *testing.T) {
	o := runCLI(t, noEnv, "", "-licenses")
	if o.code != 0 || !strings.Contains(o.stdout, "CMUdict") {
		t.Errorf("got %+v", o)
	}
}

func TestConvertTrace(t *testing.T) {
	in := writeFile(t, "in.txt", "日本語だけ\nGitHubとv2\n")
	o := runCLI(t, noEnv, "", "convert", "-missing-model=spell", "-trace", in)
	if o.code != 0 || o.stdout != "日本語だけ\nギットハブとヴィー2\n" {
		t.Fatalf("got %+v", o)
	}
	var recs []traceRecord
	for _, line := range strings.Split(strings.TrimSpace(o.stderr), "\n") {
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var r traceRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("trace line %q: %v", line, err)
		}
		recs = append(recs, r)
	}
	want := []traceRecord{
		{File: in, Line: 2, Start: 0, End: 6, Source: "GitHub", Result: "ギットハブ", Method: "dictionary", Rule: "tech:GitHub"},
		{File: in, Line: 2, Start: 9, End: 10, Source: "v", Result: "ヴィー", Method: "spell", Class: "letter"},
	}
	if len(recs) != len(want) {
		t.Fatalf("trace = %+v, want %+v", recs, want)
	}
	for i := range want {
		if recs[i] != want[i] {
			t.Errorf("trace[%d] = %+v, want %+v", i, recs[i], want[i])
		}
	}
}

func TestStatsCheck(t *testing.T) {
	corpus := writeFile(t, "corpus.md", "---\ntitle: x\n---\nGitHubでupdateする\n```go\nfunc main() {}\n```\nno Japanese here\n")
	o := runCLI(t, noEnv, "", "stats", "-check", "-missing-model=spell", corpus)
	if o.code != 0 {
		t.Fatalf("got %+v", o)
	}
	for _, w := range []string{"lines 1, with letters 1", "span kind", "piece class", "(dictionary)", "GitHub(1)", "update(1)"} {
		if !strings.Contains(o.stdout, w) {
			t.Errorf("stats output lacks %q:\n%s", w, o.stdout)
		}
	}
	if strings.Contains(o.stdout, "func") {
		t.Errorf("stats read fenced code:\n%s", o.stdout)
	}

	o = runCLI(t, noEnv, "", "stats", "-missing-model=spell")
	if o.code != 1 || !strings.Contains(o.stderr, "no corpus files") {
		t.Errorf("no files: got %+v", o)
	}
}

// lineWriter is a concurrency-safe io.Writer that also sends each written
// chunk to a channel.
type lineWriter struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	chunks chan string
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)
	select {
	case w.chunks <- string(p):
	default:
	}
	return len(p), nil
}

func TestServe(t *testing.T) {
	received := make(chan []byte, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- body
		w.Header().Set("Content-Type", "audio/wav")
		io.WriteString(w, "RIFF")
	}))
	defer upstream.Close()

	ctx, cancel := context.WithCancel(context.Background())
	stderr := &lineWriter{chunks: make(chan string, 16)}
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, []string{"serve", "-listen", "127.0.0.1:0", "-upstream", upstream.URL, "-missing-model=spell"},
			noEnv, strings.NewReader(""), io.Discard, stderr)
	}()

	addr := ""
	re := regexp.MustCompile(`listening on (http://[^,\s]+)`)
	for addr == "" {
		select {
		case chunk := <-stderr.chunks:
			if m := re.FindStringSubmatch(chunk); m != nil {
				addr = m[1]
			}
		case code := <-done:
			t.Fatalf("serve exited with %d: %s", code, stderr.buf.String())
		case <-time.After(10 * time.Second):
			t.Fatal("serve did not start")
		}
	}

	req := `{"model":"irodori","input":"GitHubのREADMEをupdateして、2.4GHzのWi-Fiで送る","voice":"a","response_format":"wav"}`
	resp, err := http.Post(addr+"/v1/audio/speech", "application/json", strings.NewReader(req))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "RIFF" {
		t.Errorf("response %d %q", resp.StatusCode, body)
	}

	var got struct {
		Model, Input, Voice string
		Format              string `json:"response_format"`
	}
	select {
	case b := <-received:
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("upstream body %q: %v", b, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("upstream received nothing")
	}
	if script.HasLetter(got.Input) {
		t.Errorf("upstream input %q has a letter", got.Input)
	}
	if want := "ギットハブのリードミーをユーピーディーエーティーイーして、2.4ギガヘルツのワイファイで送る"; got.Input != want {
		t.Errorf("upstream input = %q, want %q", got.Input, want)
	}
	if got.Model != "irodori" || got.Voice != "a" || got.Format != "wav" {
		t.Errorf("other members changed: %+v", got)
	}

	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("serve exited with %d: %s", code, stderr.buf.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not shut down")
	}
}

func TestServeNeedsUpstream(t *testing.T) {
	o := runCLI(t, noEnv, "", "serve", "-missing-model=spell")
	if o.code != 1 || !strings.Contains(o.stderr, "-upstream") {
		t.Errorf("got %+v", o)
	}
}
