package ngram

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"
)

func TestBuiltin(t *testing.T) {
	zr, err := gzip.NewReader(bytes.NewReader(builtinData))
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	if _, err := io.Copy(h, zr); err != nil {
		t.Fatal(err)
	}
	const want = "356be761aba65e89449fe97ef3b4a3324921a8367f7365e672298e5dea720ee2"
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		t.Fatalf("bundled ngram.json sha256 = %s, want %s", got, want)
	}

	m, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	checkScores(t, m, loadCases(t, "testdata/real-ngram.json"))
	if again, _ := Builtin(); again != m {
		t.Error("Builtin returned a different Model on the second call")
	}
}

func TestNotices(t *testing.T) {
	if n := Notices(); !strings.Contains(n, "Carnegie Mellon University") || !strings.Contains(n, "Redistributions in binary form") {
		t.Error("Notices lacks the CMUdict copyright notice and license terms")
	}
}
