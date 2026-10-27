package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/tenkoh/go-yomikae/proxy"
)

func runServe(ctx context.Context, e env, cfg *config, fs *flag.FlagSet, args []string) error {
	listen := fs.String("listen", "127.0.0.1:8081", "address to listen on")
	upstream := fs.String("upstream", "", "URL of the TTS server, such as http://127.0.0.1:8080 (required)")
	maxBody := fs.Int64("max-body", proxy.DefaultMaxBody, "largest speech request body in bytes")
	if err := cfg.parse(e, fs, args); err != nil {
		return err
	}
	if *upstream == "" {
		return errors.New("serve: -upstream is required")
	}
	u, err := url.Parse(*upstream)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("serve: -upstream %q is not an http or https URL", *upstream)
	}
	if *maxBody <= 0 {
		return fmt.Errorf("serve: -max-body is %d, want > 0", *maxBody)
	}
	c, err := cfg.converter(e)
	if err != nil {
		return err
	}

	logger := log.New(e.stderr, "yomikae: ", log.LstdFlags)
	convert := func(ctx context.Context, s string) (string, error) {
		r, err := c.Convert(ctx, s)
		return r.Text, err
	}
	srv := &http.Server{
		Handler:           proxy.New(u, convert, proxy.Options{MaxBody: *maxBody, ErrorLog: logger}),
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          logger,
	}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	logger.Printf("listening on http://%s, forwarding to %s", ln.Addr(), u)

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
