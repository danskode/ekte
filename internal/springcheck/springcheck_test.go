package springcheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMavenCmdDefaultMvn sikrer at det repo-leverede ./mvnw ALDRIG køres uden
// eksplicit opt-in: i et klonet repo er det angriber-kontrolleret kode.
func TestMavenCmdDefaultMvn(t *testing.T) {
	dir := t.TempDir()
	mvnw := filepath.Join(dir, "mvnw")
	if err := os.WriteFile(mvnw, []byte("#!/bin/sh\necho pwned\n"), 0755); err != nil {
		t.Fatal(err)
	}

	// Uden samtykke: system-'mvn', selv om en eksekverbar ./mvnw findes.
	t.Setenv("EKTE_ALLOW_LOCAL_HOOKS", "")
	if got := mavenCmd(dir); got != "mvn" {
		t.Errorf("uden samtykke: mavenCmd = %q, forventet \"mvn\" (./mvnw må ikke køres)", got)
	}

	// Med eksplicit opt-in: den eksekverbare wrapper må bruges.
	t.Setenv("EKTE_ALLOW_LOCAL_HOOKS", "1")
	if got := mavenCmd(dir); got != mvnw {
		t.Errorf("med EKTE_ALLOW_LOCAL_HOOKS: mavenCmd = %q, forventet %q", got, mvnw)
	}

	// Opt-in men ingen wrapper: stadig system-'mvn'.
	empty := t.TempDir()
	if got := mavenCmd(empty); got != "mvn" {
		t.Errorf("ingen ./mvnw: mavenCmd = %q, forventet \"mvn\"", got)
	}
}

func TestExtractLinks(t *testing.T) {
	// action= skal IKKE med — form-actions er typisk POST-only og giver
	// falske 405'er ved GET-crawl.
	html := `<a href="/om">Om</a> <img src="/img/logo.png"> <form action="/kontakt">
	<a href="https://ekstern.dk/x">ekstern</a> <a href="/om">dublet</a> <a href="#top">anker</a>`
	links := ExtractLinks(html)
	want := []string{"/om", "/img/logo.png"}
	if len(links) != len(want) {
		t.Fatalf("forventet %v, fik %v", want, links)
	}
	for i := range want {
		if links[i] != want[i] {
			t.Errorf("link %d: forventet %s, fik %s", i, want[i], links[i])
		}
	}
}

func TestIsWhitelabel(t *testing.T) {
	if !IsWhitelabel("<h1>Whitelabel Error Page</h1>") {
		t.Error("Whitelabel-side burde genkendes")
	}
	if IsWhitelabel("<h1>Velkommen</h1>") {
		t.Error("normal side burde ikke genkendes som Whitelabel")
	}
}

func TestReadServerPort(t *testing.T) {
	dir := t.TempDir()
	res := filepath.Join(dir, "src", "main", "resources")
	if err := os.MkdirAll(res, 0755); err != nil {
		t.Fatal(err)
	}
	if got := ReadServerPort(dir); got != 8080 {
		t.Errorf("uden properties: forventet 8080, fik %d", got)
	}
	if err := os.WriteFile(filepath.Join(res, "application.properties"),
		[]byte("spring.application.name=demo\nserver.port=9091\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := ReadServerPort(dir); got != 9091 {
		t.Errorf("forventet 9091, fik %d", got)
	}
}

func TestScanControllerPaths(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "src", "main", "java", "dk", "demo")
	if err := os.MkdirAll(pkg, 0755); err != nil {
		t.Fatal(err)
	}
	src := `package dk.demo;
public class Ctrl {
  @GetMapping("/varer")
  public String varer() { return "varer"; }
  @GetMapping("/varer/{id}")
  public String vare() { return "vare"; }
  @RequestMapping(value = "/admin")
  public String admin() { return "admin"; }
}`
	prefixed := `package dk.demo;
@Controller
@RequestMapping("/admin")
public class AdminCtrl {
  @GetMapping("/posts")
  public String posts() { return "posts"; }
}`
	if err := os.WriteFile(filepath.Join(pkg, "Ctrl.java"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "AdminCtrl.java"), []byte(prefixed), 0644); err != nil {
		t.Fatal(err)
	}
	paths := ScanControllerPaths(dir)
	// {id}-stien skal IKKE med; klasse-prefix skal anvendes. Rækkefølgen
	// afhænger af filsystem-vandringen, så der tjekkes mængde-medlemskab.
	want := map[string]bool{"/varer": true, "/admin": true, "/admin/posts": true}
	if len(paths) != len(want) {
		t.Fatalf("forventet %d stier %v, fik %v", len(want), want, paths)
	}
	for _, p := range paths {
		if !want[p] {
			t.Errorf("uventet sti: %s", p)
		}
	}
}

func TestCrawl(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			// Spring-stil: ukendt rute giver Whitelabel med 404.
			w.WriteHeader(404)
			w.Write([]byte("<h1>Whitelabel Error Page</h1>"))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<a href="/om">om</a> <a href="/død">død</a>`))
	})
	mux.HandleFunc("/om", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<h1>Om os</h1>"))
	})
	mux.HandleFunc("/whitelabel200", func(w http.ResponseWriter, r *http.Request) {
		// Whitelabel kan også komme med HTTP 200 (fx template-fejl håndteret bredt).
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("Whitelabel Error Page — der opstod en fejl"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	failures, visited := Crawl(context.Background(), srv.URL, []string{"/", "/whitelabel200"})
	if visited != 4 {
		t.Errorf("forventet 4 besøgte sider (/, /whitelabel200, /om, /død), fik %d", visited)
	}
	if len(failures) != 2 {
		t.Fatalf("forventet 2 fejl (død + whitelabel200), fik %v", failures)
	}
	joined := strings.Join(failures, "\n")
	if !strings.Contains(joined, "/død") || !strings.Contains(joined, "/whitelabel200") {
		t.Errorf("fejlene burde nævne /død og /whitelabel200: %v", failures)
	}

	// Ren side → ingen fejl.
	failures, _ = Crawl(context.Background(), srv.URL, []string{"/om"})
	if len(failures) != 0 {
		t.Errorf("ren side burde ikke fejle: %v", failures)
	}
}

func TestRunUdenPom(t *testing.T) {
	rep := Run(context.Background(), t.TempDir(), "")
	if rep.OK {
		t.Error("mappe uden pom.xml burde fejle")
	}
	if !strings.Contains(strings.Join(rep.Lines, " "), "pom.xml") {
		t.Errorf("fejlen burde nævne pom.xml: %v", rep.Lines)
	}
}

// TestLoginAndCrawl: det autentificerede flow skal logge ind (med CSRF),
// crawle bag loginet og fange whitelabel-sider dér — inkl. det "nøgne"
// klasse-prefix (/admin), som brugere navigerer til efter login.
func TestLoginAndCrawl(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "src", "main", "java", "dk", "demo")
	if err := os.MkdirAll(pkg, 0755); err != nil {
		t.Fatal(err)
	}
	ctrl := `package dk.demo;
@Controller
@RequestMapping("/admin")
public class AdminCtrl {
  @GetMapping("/posts")
  public String posts() { return "posts"; }
}`
	if err := os.WriteFile(filepath.Join(pkg, "AdminCtrl.java"), []byte(ctrl), 0644); err != nil {
		t.Fatal(err)
	}

	loggedIn := false
	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			r.ParseForm()
			if r.FormValue("username") == "admin" && r.FormValue("password") == "admin" && r.FormValue("_csrf") == "tok123" {
				loggedIn = true
				http.SetCookie(w, &http.Cookie{Name: "SESSION", Value: "x"})
				http.Redirect(w, r, "/", http.StatusFound)
				return
			}
			http.Redirect(w, r, "/login?error", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<form><input type="hidden" name="_csrf" value="tok123"/></form>`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			// Spring-stil: ukendt rute → Whitelabel 404. Uden dette fanger
			// Go's "/"-mønster alle ukendte stier og maskerer 404-scenariet.
			w.WriteHeader(404)
			w.Write([]byte("Whitelabel Error Page"))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<h1>Forside</h1>"))
	})
	mux.HandleFunc("/admin/posts", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<h1>Admin posts</h1>"))
	})
	// Bemærk: ingen handler for GET /admin → 404 (brugerens whitelabel-scenarie)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	failures, visited := loginAndCrawl(context.Background(), srv.URL, dir, "")
	if !loggedIn {
		t.Fatal("login blev aldrig udført (CSRF/form-håndtering fejlede)")
	}
	if visited == 0 {
		t.Fatal("autentificeret crawl besøgte ingen sider")
	}
	joined := strings.Join(failures, "\n")
	if !strings.Contains(joined, "/admin") || !strings.Contains(joined, "efter login") {
		t.Errorf("manglende /admin-mapping burde fanges som [efter login]-fejl, fik: %v", failures)
	}
	if strings.Contains(joined, "/admin/posts —") {
		t.Errorf("/admin/posts virker og burde ikke fejle: %v", failures)
	}

	// Forkerte credentials → tydelig fejl.
	failures, _ = loginAndCrawl(context.Background(), srv.URL, dir, "admin:forkert")
	if len(failures) == 0 || !strings.Contains(failures[0], "afvist") {
		t.Errorf("afvist login burde rapporteres, fik: %v", failures)
	}
}

func TestFindSingleMavenSubdir(t *testing.T) {
	// Én undermappe med pom.xml → findes.
	dir := t.TempDir()
	sub := filepath.Join(dir, "app")
	os.MkdirAll(sub, 0755)
	os.WriteFile(filepath.Join(sub, "pom.xml"), []byte("<project/>"), 0644)
	if got := findSingleMavenSubdir(dir); got != sub {
		t.Errorf("forventet %s, fik %q", sub, got)
	}
	// To kandidater → tvetydigt, returnér "".
	sub2 := filepath.Join(dir, "app2")
	os.MkdirAll(sub2, 0755)
	os.WriteFile(filepath.Join(sub2, "pom.xml"), []byte("<project/>"), 0644)
	if got := findSingleMavenSubdir(dir); got != "" {
		t.Errorf("tvetydighed burde give \"\", fik %q", got)
	}
	// Ingen → "".
	if got := findSingleMavenSubdir(t.TempDir()); got != "" {
		t.Errorf("ingen pom burde give \"\", fik %q", got)
	}
}

func TestRunMavenIUndermappe(t *testing.T) {
	// Run skal finde pom.xml i en enkelt undermappe og rapportere det —
	// uden at fejle på "pom.xml ikke fundet" (compile fejler dog uden mvn,
	// men beskeden skal vise at undermappen blev brugt).
	dir := t.TempDir()
	sub := filepath.Join(dir, "guestbook-app")
	os.MkdirAll(sub, 0755)
	os.WriteFile(filepath.Join(sub, "pom.xml"), []byte("<project/>"), 0644)
	rep := Run(context.Background(), dir, "")
	if rep.OK {
		t.Skip("mvn tilgængelig og projekt byggede uventet — ikke relevant her")
	}
	if !strings.Contains(strings.Join(rep.Lines, " "), "guestbook-app") {
		t.Errorf("rapporten burde nævne undermappen, fik: %v", rep.Lines)
	}
}
