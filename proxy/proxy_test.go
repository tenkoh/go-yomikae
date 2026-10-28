package proxy

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type received struct {
	method, path, query string
	header              http.Header
	contentLength       int64
	body                []byte
}

// recorder is a mock upstream that records each request and answers with
// a fixed body.
func recorder(t *testing.T) (*httptest.Server, <-chan received) {
	t.Helper()
	ch := make(chan received, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("upstream: reading body: %v", err)
		}
		ch <- received{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Clone(), r.ContentLength, body}
		w.Header().Set("X-Sample-Rate", "24000")
		io.WriteString(w, "upstream response")
	}))
	t.Cleanup(srv.Close)
	return srv, ch
}

func startProxy(t *testing.T, upstream *httptest.Server, convert func(context.Context, string) (string, error), opts Options) *httptest.Server {
	t.Helper()
	u, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	p := httptest.NewServer(New(u, convert, opts))
	t.Cleanup(p.Close)
	return p
}

func upper(_ context.Context, s string) (string, error) { return strings.ToUpper(s), nil }

func mustNotConvert(t *testing.T) func(context.Context, string) (string, error) {
	return func(_ context.Context, s string) (string, error) {
		t.Errorf("convert(%q) called", s)
		return s, nil
	}
}

func post(t *testing.T, p *httptest.Server, path, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(p.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func recv(t *testing.T, ch <-chan received) received {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("upstream received no request")
		return received{}
	}
}

func TestSpeechInputRewritten(t *testing.T) {
	tests := map[string]struct{ in, want string }{
		"other members keep their bytes": {
			in: "{ \"model\" :\"irodori\",\t\"input\" :  \"hello <b> \\\"q\\\"\" ,\n" +
				`  "voice":"あA", "speed": 1.50e0, "instructions":"a\/b", "seed":[ 1 ,2 ]}`,
			want: "{ \"model\" :\"irodori\",\t\"input\" :  \"HELLO <B> \\\"Q\\\"\" ,\n" +
				`  "voice":"あA", "speed": 1.50e0, "instructions":"a\/b", "seed":[ 1 ,2 ]}`,
		},
		"escaped input is decoded": {
			in:   `{"input":"café 😀"}`,
			want: `{"input":"CAFÉ 😀"}`,
		},
		"duplicate inputs, nested ignored": {
			in:   `{"input":"a","nested":{"input":"x"},"input":"b"}`,
			want: `{"input":"A","nested":{"input":"x"},"input":"B"}`,
		},
		"escaped key": {
			in:   `{"input":"a"}`,
			want: `{"input":"A"}`,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			up, ch := recorder(t)
			p := startProxy(t, up, upper, Options{})
			post(t, p, SpeechPath, tt.in)
			got := recv(t, ch)
			if string(got.body) != tt.want {
				t.Errorf("upstream body\n got %s\nwant %s", got.body, tt.want)
			}
			if got.contentLength != int64(len(tt.want)) {
				t.Errorf("ContentLength = %d, want %d", got.contentLength, len(tt.want))
			}
		})
	}
}

func TestSpeechHeadersKept(t *testing.T) {
	up, ch := recorder(t)
	p := startProxy(t, up, upper, Options{})

	// An unsized body arrives chunked; upstream must still get a length.
	body := io.NopCloser(strings.NewReader(`{"input":"hi","model":"m"}`))
	req, err := http.NewRequest(http.MethodPost, p.URL+SpeechPath+"?q=1", body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer sk-test")
	req.Header.Add("X-Custom", "one")
	req.Header.Add("X-Custom", "two")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	got := recv(t, ch)
	const want = `{"input":"HI","model":"m"}`
	if string(got.body) != want {
		t.Errorf("upstream body = %s, want %s", got.body, want)
	}
	if got.contentLength != int64(len(want)) || got.header.Get("Content-Length") != strconv.Itoa(len(want)) {
		t.Errorf("Content-Length = %d / %q, want %d", got.contentLength, got.header.Get("Content-Length"), len(want))
	}
	for key, want := range map[string][]string{
		"Content-Type":  {"application/json; charset=utf-8"},
		"Authorization": {"Bearer sk-test"},
		"X-Custom":      {"one", "two"},
	} {
		if got := got.header.Values(key); !equal(got, want) {
			t.Errorf("upstream %s = %q, want %q", key, got, want)
		}
	}
	for _, key := range []string{"X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "Forwarded"} {
		if v := got.header.Get(key); v != "" {
			t.Errorf("upstream got %s: %q", key, v)
		}
	}
	if got.query != "q=1" {
		t.Errorf("upstream query = %q, want q=1", got.query)
	}
	if v := resp.Header.Get("X-Sample-Rate"); v != "24000" {
		t.Errorf("response X-Sample-Rate = %q, want 24000", v)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSpeechStreamsWithoutBuffering(t *testing.T) {
	// speech.cpp streams pcm chunks as well as SSE events.
	for _, ct := range []string{"text/event-stream", "audio/pcm"} {
		t.Run(ct, func(t *testing.T) { testStreaming(t, ct) })
	}
}

func testStreaming(t *testing.T, contentType string) {
	const (
		event1 = "data: {\"type\":\"speech.audio.delta\",\"audio\":\"AAAA\"}\n\n"
		event2 = "data: {\"type\":\"speech.audio.delta\",\"audio\":\"BBBB\"}\n\n"
		done   = "data: {\"type\":\"speech.audio.done\",\"seed\":42,\"samples\":4,\"stop\":\"complete\"}\n\n"
	)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		io.WriteString(w, event1)
		w.(http.Flusher).Flush()
		<-release
		io.WriteString(w, event2)
		w.(http.Flusher).Flush()
		io.WriteString(w, done)
	}))
	t.Cleanup(up.Close)
	t.Cleanup(unblock) // runs before up.Close, so a failing test does not hang
	p := startProxy(t, up, upper, Options{})

	resp := post(t, p, SpeechPath, `{"input":"hi","stream_format":"sse"}`)
	if ct := resp.Header.Get("Content-Type"); ct != contentType {
		t.Errorf("Content-Type = %q, want %q", ct, contentType)
	}
	br := bufio.NewReader(resp.Body)
	first := make(chan string, 1)
	go func() {
		var b strings.Builder
		for b.Len() < len(event1) {
			line, err := br.ReadString('\n')
			b.WriteString(line)
			if err != nil {
				break
			}
		}
		first <- b.String()
	}()
	select {
	case got := <-first:
		if got != event1 {
			t.Fatalf("first event = %q, want %q", got, event1)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first event did not arrive before upstream continued")
	}

	unblock()
	rest, err := io.ReadAll(br)
	if err != nil {
		t.Fatal(err)
	}
	if string(rest) != event2+done {
		t.Errorf("rest = %q, want %q", rest, event2+done)
	}
}

func TestOtherEndpointsPassThrough(t *testing.T) {
	t.Run("GET /v1/models", func(t *testing.T) {
		up, ch := recorder(t)
		p := startProxy(t, up, mustNotConvert(t), Options{})
		resp, err := http.Get(p.URL + "/v1/models?limit=2")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		got := recv(t, ch)
		if got.method != http.MethodGet || got.path != "/v1/models" || got.query != "limit=2" {
			t.Errorf("upstream got %s %s?%s", got.method, got.path, got.query)
		}
		if b, _ := io.ReadAll(resp.Body); string(b) != "upstream response" {
			t.Errorf("response = %q", b)
		}
	})

	t.Run("POST /v1/audio/transcriptions", func(t *testing.T) {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		fw, err := mw.CreateFormFile("file", "a.wav")
		if err != nil {
			t.Fatal(err)
		}
		fw.Write([]byte("RIFF\x00\x01{\"input\":\"x\"}"))
		mw.WriteField("model", "whisper")
		mw.Close()

		up, ch := recorder(t)
		p := startProxy(t, up, mustNotConvert(t), Options{})
		resp, err := http.Post(p.URL+"/v1/audio/transcriptions", mw.FormDataContentType(), bytes.NewReader(body.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		got := recv(t, ch)
		if !bytes.Equal(got.body, body.Bytes()) {
			t.Errorf("upstream body differs\n got %q\nwant %q", got.body, body.Bytes())
		}
		if ct := got.header.Get("Content-Type"); ct != mw.FormDataContentType() {
			t.Errorf("Content-Type = %q, want %q", ct, mw.FormDataContentType())
		}
	})

	t.Run("GET /v1/realtime upgrade", func(t *testing.T) {
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Upgrade") != "websocket" {
				http.Error(w, "no upgrade", http.StatusBadRequest)
				return
			}
			conn, rw, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
			rw.Flush()
			buf := make([]byte, 4)
			if _, err := io.ReadFull(rw, buf); err != nil {
				return
			}
			rw.Write(buf)
			rw.Flush()
		}))
		t.Cleanup(up.Close)
		p := startProxy(t, up, mustNotConvert(t), Options{})

		conn, err := net.Dial("tcp", p.Listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		io.WriteString(conn, "GET /v1/realtime HTTP/1.1\r\nHost: proxy\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		br := bufio.NewReader(conn)
		resp, err := http.ReadResponse(br, nil)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusSwitchingProtocols {
			t.Fatalf("status = %d, want 101", resp.StatusCode)
		}
		io.WriteString(conn, "ping")
		buf := make([]byte, 4)
		if _, err := io.ReadFull(br, buf); err != nil {
			t.Fatal(err)
		}
		if string(buf) != "ping" {
			t.Errorf("echo = %q, want ping", buf)
		}
	})
}

func TestSpeechUnrewritableForwardedUnchanged(t *testing.T) {
	for name, in := range map[string]string{
		"truncated":        `{"input":"a",`,
		"not json":         `input=a`,
		"array":            `["input","a"]`,
		"number input":     `{"input":42}`,
		"null input":       `{"input":null}`,
		"object input":     `{"input":{"text":"a"}}`,
		"trailing garbage": `{"input":"a"} x`,
		"two objects":      `{"input":"a"}{"input":"b"}`,
		"no input":         `{"model":"m"}`,
		"empty":            ``,
	} {
		t.Run(name, func(t *testing.T) {
			up, ch := recorder(t)
			p := startProxy(t, up, mustNotConvert(t), Options{})
			post(t, p, SpeechPath, in)
			if got := recv(t, ch); string(got.body) != in {
				t.Errorf("upstream body = %q, want %q", got.body, in)
			}
		})
	}
}

func TestSpeechBodyLimit(t *testing.T) {
	const limit = 64
	at := `{"input":"a","pad":"` + strings.Repeat("x", limit-22) + `"}`
	if len(at) != limit {
		t.Fatalf("len(at) = %d", len(at))
	}

	up, ch := recorder(t)
	p := startProxy(t, up, upper, Options{MaxBody: limit})
	if resp := post(t, p, SpeechPath, at); resp.StatusCode != http.StatusOK {
		t.Errorf("body at limit: status = %d, want 200", resp.StatusCode)
	}
	recv(t, ch)

	over := at[:len(at)-2] + `x"}`
	if resp := post(t, p, SpeechPath, over); resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("body over limit: status = %d, want 413", resp.StatusCode)
	}
	select {
	case r := <-ch:
		t.Errorf("upstream received %q", r.body)
	default:
	}
}

func TestSpeechConvertError(t *testing.T) {
	up, ch := recorder(t)
	failing := func(context.Context, string) (string, error) { return "", context.Canceled }
	p := startProxy(t, up, failing, Options{})
	if resp := post(t, p, SpeechPath, `{"input":"a"}`); resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", resp.StatusCode)
	}
	select {
	case r := <-ch:
		t.Errorf("upstream received %q", r.body)
	default:
	}
}
