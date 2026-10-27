// Package proxy is an OpenAI-compatible reverse proxy that rewrites the input
// text of speech requests before they reach a TTS server such as speech.cpp.
package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
)

// SpeechPath is the endpoint whose requests have their input rewritten.
const SpeechPath = "/v1/audio/speech"

// DefaultMaxBody is the speech request body limit when Options.MaxBody is 0.
const DefaultMaxBody = 1 << 20

// Options configures New. The zero value is ready to use.
type Options struct {
	// MaxBody is the largest speech request body in bytes. Larger bodies get
	// 413 Request Entity Too Large. Zero means DefaultMaxBody.
	MaxBody int64
	// ErrorLog receives proxy errors. Nil means the log package's standard
	// logger.
	ErrorLog *log.Logger
}

type handler struct {
	rp      *httputil.ReverseProxy
	convert func(ctx context.Context, s string) (string, error)
	maxBody int64
	errLog  *log.Logger
}

// New returns a handler that forwards every request to upstream. For POST
// /v1/audio/speech it replaces the string value of each top-level "input"
// member of the JSON body with convert's result, leaving every other byte of
// the body unchanged. Responses stream to the client as they arrive.
func New(upstream *url.URL, convert func(ctx context.Context, s string) (string, error), opts Options) http.Handler {
	if opts.MaxBody == 0 {
		opts.MaxBody = DefaultMaxBody
	}
	return &handler{
		rp: &httputil.ReverseProxy{
			// Rewrite, unlike Director, adds no X-Forwarded-* headers.
			Rewrite:       func(pr *httputil.ProxyRequest) { pr.SetURL(upstream) },
			FlushInterval: -1,
			ErrorLog:      opts.ErrorLog,
		},
		convert: convert,
		maxBody: opts.MaxBody,
		errLog:  opts.ErrorLog,
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost && r.URL.Path == SpeechPath {
		var ok bool
		if r, ok = h.rewriteRequest(w, r); !ok {
			return
		}
	}
	h.rp.ServeHTTP(w, r)
}

func (h *handler) rewriteRequest(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.maxBody))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "reading request body: "+err.Error(), http.StatusBadRequest)
		}
		return nil, false
	}
	body, err = rewriteInput(r.Context(), body, h.convert)
	if err != nil {
		// convert fails only when the client is gone; 502 matches what
		// ReverseProxy itself writes when the request is canceled upstream.
		h.logf("proxy: converting input: %v", err)
		w.WriteHeader(http.StatusBadGateway)
		return nil, false
	}
	r = r.Clone(r.Context())
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	r.TransferEncoding = nil
	r.Header.Set("Content-Length", strconv.Itoa(len(body)))
	return r, true
}

func (h *handler) logf(format string, args ...any) {
	if h.errLog != nil {
		h.errLog.Printf(format, args...)
	} else {
		log.Printf(format, args...)
	}
}

// rewriteInput returns body with the string value of each top-level "input"
// member replaced by convert's result. It returns body unchanged if body is
// not a single JSON object.
func rewriteInput(ctx context.Context, body []byte, convert func(context.Context, string) (string, error)) ([]byte, error) {
	type span struct{ start, end int }
	var spans []span

	dec := json.NewDecoder(bytes.NewReader(body))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return body, nil
	}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return body, nil
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return body, nil
		}
		if key == "input" && v[0] == '"' {
			end := int(dec.InputOffset())
			spans = append(spans, span{end - len(v), end})
		}
	}
	if _, err := dec.Token(); err != nil {
		return body, nil
	}
	if _, err := dec.Token(); err != io.EOF {
		return body, nil
	}
	if len(spans) == 0 {
		return body, nil
	}

	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	prev := 0
	for _, sp := range spans {
		var s string
		if err := json.Unmarshal(body[sp.start:sp.end], &s); err != nil {
			return nil, err
		}
		s, err := convert(ctx, s)
		if err != nil {
			return nil, err
		}
		out.Write(body[prev:sp.start])
		if err := enc.Encode(s); err != nil {
			return nil, err
		}
		out.Truncate(out.Len() - 1) // Encode appends a newline.
		prev = sp.end
	}
	out.Write(body[prev:])
	return out.Bytes(), nil
}
