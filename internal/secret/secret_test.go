package secret

import (
	"strings"
	"testing"
)

// Test-secrets konstrueres ved runtime (sammensætning), så ingen literal
// secret-token står i kildefilen — ellers udløser GitHubs push protection
// (server-side secret scanning) på selve testfilen.

func TestRedact(t *testing.T) {
	aws := "AKIA" + strings.Repeat("Z", 16)
	val := strings.Repeat("x", 20)
	apikey := `api_key = "` + val + `"`
	in := apikey + "\n" + aws + "\nfunc add(a, b int) int { return a + b }"

	out, n := Redact(in)
	if n < 2 {
		t.Errorf("forventede ≥2 redaktioner, fik %d:\n%s", n, out)
	}
	if strings.Contains(out, aws) || strings.Contains(out, val) {
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

func TestRedactMoreFormats(t *testing.T) {
	google := "AIza" + strings.Repeat("a", 35)
	stripe := "sk_" + "live_" + strings.Repeat("b", 24)
	pass := strings.Repeat("p", 10)
	conn := "postgres://admin:" + pass + "@db.example.com:5432/app"

	for name, in := range map[string]string{"google": google, "stripe": stripe, "connstring": conn} {
		if out, n := Redact(in); n == 0 {
			t.Errorf("%s: forventede redaktion, fik 0 (%q)", name, out)
		}
	}

	// connection string: kun password maskeres, scheme/host bevares.
	out, _ := Redact(conn)
	if strings.Contains(out, pass) {
		t.Errorf("password ikke maskeret: %s", out)
	}
	if !strings.Contains(out, "db.example.com") {
		t.Errorf("host burde bevares: %s", out)
	}
}
