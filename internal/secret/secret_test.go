package secret

import (
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	in := `api_key = "abcdef0123456789ABCDEF"
AKIAIOSFODNN7EXAMPLE
func add(a, b int) int { return a + b }`
	out, n := Redact(in)
	if n < 2 {
		t.Errorf("forventede ≥2 redaktioner, fik %d:\n%s", n, out)
	}
	if strings.Contains(out, "abcdef0123456789ABCDEF") || strings.Contains(out, "AKIAIOSFODNN7EXAMPLE") {
		t.Errorf("secret blev ikke redakteret:\n%s", out)
	}
	if !strings.Contains(out, "func add(a, b int) int") {
		t.Errorf("almindelig kode burde bevares:\n%s", out)
	}
}

func TestRedactCleanCode(t *testing.T) {
	in := "for i := 0; i < n; i++ { total += i }"
	if out, n := Redact(in); n != 0 || out != in {
		t.Errorf("ren kode burde ikke redakteres: n=%d out=%q", n, out)
	}
}
