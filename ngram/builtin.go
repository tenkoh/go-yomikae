package ngram

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"sync"
)

// The upstream release ships ngram.json in an LZMA zip, which the standard
// library cannot read, so data/ holds the same JSON recompressed with gzip.
//
//go:embed data/ngram.json.gz
var builtinData []byte

//go:embed NOTICES.md
var notices string

var builtin = sync.OnceValues(func() (*Model, error) {
	zr, err := gzip.NewReader(bytes.NewReader(builtinData))
	if err != nil {
		return nil, err
	}
	return Load(zr)
})

// Builtin returns the model bundled with this package, the ngram.json of
// Patchethium/e2k v0.8.1. It loads the model on the first call and returns the
// same Model afterwards.
func Builtin() (*Model, error) {
	return builtin()
}

// Notices returns the copyright notices and license terms that a distribution
// of the bundled model must reproduce.
func Notices() string {
	return notices
}
