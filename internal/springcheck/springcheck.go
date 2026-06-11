// Package springcheck implementerer goal-tjekket for Java + Thymeleaf-projekter:
// (1) mvn compile uden fejl, (2) appen starter, (3) alle interne links i
// frontenden og alle simple GET-endpoints fra controllerne svarer uden
// Spring Boots "Whitelabel Error Page" eller HTTP-fejlstatus, (4) findes en
// login-side, testes det AUTENTIFICEREDE flow også: login udføres (default
// admin/admin) og de beskyttede sider crawles bag loginet — fejl efter login
// er ellers usynlige for et uautentificeret tjek (alt redirecter blot til /login).
// Køres som `ekte springcheck [bruger:kode]` — typisk som goal.check_hook.
package springcheck

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

type Report struct {
	OK    bool
	Lines []string // menneskelæsbar rapport (dansk)
	URL   string   // tilgængelig adresse ved succes (lokal IP + port)
}

const (
	maxPages       = 100
	requestTimeout = 10 * time.Second
	bootTimeout    = 150 * time.Second
)

// Run udfører hele tjekket i projektmappen dir. login er "bruger:kode" til
// det autentificerede flow (tom = default admin:admin).
func Run(ctx context.Context, dir string, login string) Report {
	if _, err := os.Stat(filepath.Join(dir, "pom.xml")); err != nil {
		// Små modeller lægger ofte projektet i en undermappe (guestbook-app/,
		// blog-app/) trods instruktion om projektroden. Find en enkelt
		// pom.xml-bærende undermappe og kør tjekket dér i stedet for at fejle —
		// så bruges en korrekt bygget app, uanset hvor modellen lagde den.
		if sub := findSingleMavenSubdir(dir); sub != "" {
			rel, _ := filepath.Rel(dir, sub)
			rep := Run(ctx, sub, login)
			rep.Lines = append([]string{"ℹ Maven-projekt fundet i undermappen " + rel + "/ — tjekket dér."}, rep.Lines...)
			return rep
		}
		return fail("pom.xml ikke fundet — springcheck understøtter kun Maven-projekter (Java + Thymeleaf)")
	}

	// 1) Kompilér. -q: kun fejl-output er interessant for agenten.
	mvn := mavenCmd(dir)
	compile := exec.CommandContext(ctx, mvn, "-q", "compile")
	compile.Dir = dir
	if out, err := compile.CombinedOutput(); err != nil {
		return fail("✗ compile-fejl:\n" + tail(string(out), 4000))
	}

	port := ReadServerPort(dir)
	base := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Porten SKAL være fri inden start — ellers svarer en gammel instans på
	// waitForPort, og tjekket crawler forældet kode i stedet for den netop
	// kompilerede (observeret: 5 goal-iterationer spildt på en korrekt
	// rettelse, fordi en hængende app holdt porten og blev testet i stedet).
	if conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second); err == nil {
		conn.Close()
		return fail(fmt.Sprintf("✗ port %d er allerede optaget — en gammel app-instans kører formentlig. "+
			"Stop den og kør tjekket igen (fx: pkill -f spring-boot:run).", port))
	}

	// 2) Start appen i egen procesgruppe så hele træet kan lukkes pænt bagefter.
	app := exec.CommandContext(ctx, mvn, "-q", "spring-boot:run")
	app.Dir = dir
	app.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// 128 KB: én enkelt Spring-stacktrace med filterkæde fylder let 8-16 KB,
	// og crawleren udløser én pr. fejlende side — med en lille buffer var
	// "Caused by"-linjerne (selve årsagen) skubbet ud før rapporten blev bygget.
	appOut := &boundedBuf{max: 128 * 1024}
	app.Stdout = appOut
	app.Stderr = appOut
	if err := app.Start(); err != nil {
		return fail("✗ kunne ikke starte appen: " + err.Error())
	}
	defer func() {
		// Negativ pid = hele procesgruppen (maven + java-child).
		_ = syscall.Kill(-app.Process.Pid, syscall.SIGTERM)
		done := make(chan struct{})
		go func() { _ = app.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = syscall.Kill(-app.Process.Pid, syscall.SIGKILL)
			<-done
		}
	}()

	if err := waitForPort(ctx, port, app); err != nil {
		return fail("✗ appen kom aldrig op på port " + fmt.Sprint(port) + ": " + err.Error() +
			"\n\nApp-output:\n" + tail(appOut.String(), 3000))
	}

	// 3) Crawl frontend + skannede controller-endpoints (uautentificeret).
	seeds := append([]string{"/"}, ScanControllerPaths(dir)...)
	failures, visited := Crawl(ctx, base, seeds)

	// 4) Autentificeret flow: log ind og crawl de beskyttede sider. Fejl bag
	// login er usynlige for det uautentificerede tjek — alt redirecter blot
	// pænt til /login (observeret: whitelabel efter login som tjekket godkendte).
	authFailures, authVisited := loginAndCrawl(ctx, base, dir, login)
	failures = append(failures, authFailures...)
	visited += authVisited

	if len(failures) > 0 {
		lines := []string{fmt.Sprintf("✗ %d af %d sider/endpoints fejlede:", len(failures), visited)}
		lines = append(lines, failures...)
		// Vedlæg appens eget output — stacktraces fra 500-fejl står dér, og uden
		// dem kan modellen kun gætte på årsagen (observeret: to iterationer
		// brugt på spøgelsesjagt efter "HTTP 500" uden kontekst).
		lines = append(lines, "", "App-log (fejl-uddrag):", errorExcerpt(appOut.String()))
		return Report{OK: false, Lines: lines}
	}

	// localhost er den primære, sikre adresse — tjekket talte selv kun med
	// 127.0.0.1. LAN-adressen vises kun som sekundær med en advarsel: en
	// Spring-app binder typisk 0.0.0.0, og med default-credentials (admin/admin)
	// bør brugeren ikke opfordres til at eksponere en utestet dev-app på nettet.
	localURL := fmt.Sprintf("http://localhost:%d", port)
	lines := []string{
		fmt.Sprintf("✓ compile OK, %d sider/endpoints tjekket — ingen Whitelabel-fejl eller døde links.", visited),
		"Projektet kan tilgås på: " + localURL,
	}
	if lan := localIP(); lan != "localhost" {
		lines = append(lines, fmt.Sprintf("(På LAN: http://%s:%d — kun til betroet netværk; appen er ikke sikkerhedstestet.)", lan, port))
	}
	return Report{OK: true, URL: localURL, Lines: lines}
}

func fail(msg string) Report { return Report{OK: false, Lines: []string{msg}} }

// findSingleMavenSubdir returnerer stien til en undermappe der indeholder
// præcis ét Maven-projekt (pom.xml), hvis der kun er én sådan — ellers "".
// Kun ét niveau ned, og target/ springes over. Tvetydighed (flere kandidater)
// giver "" så vi ikke gætter forkert.
func findSingleMavenSubdir(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	found := ""
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "target" || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		sub := filepath.Join(dir, e.Name())
		if _, err := os.Stat(filepath.Join(sub, "pom.xml")); err == nil {
			if found != "" {
				return "" // flere kandidater — gæt ikke
			}
			found = sub
		}
	}
	return found
}

// mavenCmd foretrækker projektets egen wrapper (./mvnw) over global mvn.
func mavenCmd(dir string) string {
	if w := filepath.Join(dir, "mvnw"); isExecutable(w) {
		return w
	}
	return "mvn"
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0111 != 0
}

// ReadServerPort læser server.port fra application.properties (default 8080).
func ReadServerPort(dir string) int {
	data, err := os.ReadFile(filepath.Join(dir, "src", "main", "resources", "application.properties"))
	if err != nil {
		return 8080
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "server.port="); ok {
			var p int
			if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d", &p); err == nil && p > 0 {
				return p
			}
		}
	}
	return 8080
}

// waitForPort poller til appen lytter — eller fejler hvis processen dør undervejs.
func waitForPort(ctx context.Context, port int, app *exec.Cmd) error {
	deadline := time.Now().Add(bootTimeout)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if app.ProcessState != nil {
			return fmt.Errorf("app-processen afsluttede før porten åbnede")
		}
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("timeout efter %s", bootTimeout)
}

var (
	// href/src med interne stier — dækker Thymeleaf-output (renderet HTML).
	// action= er bevidst UDELADT: form-actions er typisk POST-only, og en
	// GET-crawl af dem giver falske 405-fejl (observeret: /posts/{id}/delete).
	linkRe = regexp.MustCompile(`(?:href|src)\s*=\s*["'](/[^"'#?]*)`)
	// @GetMapping("/sti") og @RequestMapping("/sti") med literal sti uden {variabler}.
	mappingRe = regexp.MustCompile(`@(?:Get|Request)Mapping\s*\(\s*(?:value\s*=\s*)?"(/[^"{}]*)"`)
)

// ExtractLinks finder interne links (href/src/action der starter med "/") i HTML.
func ExtractLinks(html string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range linkRe.FindAllStringSubmatch(html, -1) {
		p := m[1]
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// ScanControllerPaths finder simple GET-stier i @GetMapping/@RequestMapping —
// kun literal-stier uden path-variabler ({id} kan ikke gættes meningsfuldt).
// Klasse-niveau @RequestMapping bruges som prefix for metode-stierne — uden
// dette rapporteres fx /posts i en /admin-controller som død, selvom den
// reelle rute er /admin/posts (falsk positiv der sender modellen på spøgelsesjagt).
func ScanControllerPaths(dir string) []string {
	var paths []string
	seen := map[string]bool{}
	root := filepath.Join(dir, "src", "main", "java")
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".java") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		src := string(data)
		classIdx := strings.Index(src, "class ")
		prefix := ""
		for _, m := range mappingRe.FindAllStringSubmatchIndex(src, -1) {
			p := src[m[2]:m[3]]
			if classIdx >= 0 && m[0] < classIdx {
				prefix = strings.TrimSuffix(p, "/")
				continue
			}
			full := prefix + p
			if full != "" && !seen[full] {
				seen[full] = true
				paths = append(paths, full)
			}
		}
		return nil
	})
	return paths
}

// ScanControllerPrefixes returnerer klasse-niveau @RequestMapping-prefixer
// (fx /admin). De testes som sider i det autentificerede flow: brugere
// navigerer til dem direkte, og uden mapping/redirect = whitelabel efter login.
func ScanControllerPrefixes(dir string) []string {
	var prefixes []string
	seen := map[string]bool{}
	root := filepath.Join(dir, "src", "main", "java")
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".java") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		src := string(data)
		classIdx := strings.Index(src, "class ")
		for _, m := range mappingRe.FindAllStringSubmatchIndex(src, -1) {
			p := src[m[2]:m[3]]
			if classIdx >= 0 && m[0] < classIdx && p != "/" && !seen[p] {
				seen[p] = true
				prefixes = append(prefixes, p)
			}
		}
		return nil
	})
	return prefixes
}

// IsWhitelabel genkender Spring Boots standard-fejlside.
func IsWhitelabel(body string) bool {
	return strings.Contains(body, "Whitelabel Error Page")
}

// Crawl besøger seeds + alle interne links fundet undervejs (BFS, maks maxPages).
// En side fejler ved HTTP >= 400 eller Whitelabel-indhold. Returnerer fejllinjer
// og antal besøgte sider.
func Crawl(ctx context.Context, base string, seeds []string) (failures []string, visited int) {
	return crawlWithClient(ctx, &http.Client{Timeout: requestTimeout}, base, seeds)
}

// crawlWithClient er Crawl med egen klient — det autentificerede flow genbruger
// cookie-jar-klienten fra loginAndCrawl, så sessionen følger med i crawlen.
func crawlWithClient(ctx context.Context, client *http.Client, base string, seeds []string) (failures []string, visited int) {
	queue := append([]string{}, seeds...)
	seen := map[string]bool{}

	for len(queue) > 0 && visited < maxPages {
		if ctx.Err() != nil {
			return failures, visited
		}
		p := queue[0]
		queue = queue[1:]
		if seen[p] {
			continue
		}
		seen[p] = true
		visited++

		resp, err := client.Get(base + p)
		if err != nil {
			failures = append(failures, fmt.Sprintf("  %s — netværksfejl: %v", p, err))
			continue
		}
		body := make([]byte, 512*1024)
		n, _ := resp.Body.Read(body)
		for n < len(body) {
			m, err := resp.Body.Read(body[n:])
			n += m
			if err != nil {
				break
			}
		}
		resp.Body.Close()
		text := string(body[:n])

		switch {
		case resp.StatusCode >= 400:
			failures = append(failures, fmt.Sprintf("  %s — HTTP %d%s", p, resp.StatusCode, bodyHint(text)))
		case IsWhitelabel(text):
			failures = append(failures, fmt.Sprintf("  %s — Whitelabel Error Page", p))
		case strings.Contains(resp.Header.Get("Content-Type"), "text/html"):
			for _, link := range ExtractLinks(text) {
				if !seen[link] {
					queue = append(queue, link)
				}
			}
		}
	}
	return failures, visited
}

// errorExcerpt udtrækker årsagslinjerne fra appens log: "Caused by", exception-
// navne og ERROR-linjer — frem for en rå hale, hvor filter-kædens stacktrace
// typisk har skubbet selve årsagen ud af vinduet.
func errorExcerpt(appLog string) string {
	var picked []string
	for _, line := range strings.Split(appLog, "\n") {
		l := strings.TrimSpace(line)
		if l == "" {
			continue
		}
		if strings.Contains(l, "Caused by") || strings.Contains(l, "ERROR") ||
			strings.Contains(l, "Exception") && !strings.HasPrefix(l, "at ") {
			picked = append(picked, l)
		}
	}
	const maxLines = 25
	if len(picked) > maxLines {
		picked = picked[len(picked)-maxLines:]
	}
	if len(picked) == 0 {
		return tail(appLog, 2000)
	}
	return strings.Join(picked, "\n")
}

var csrfRe = regexp.MustCompile(`name="_csrf"[^>]*value="([^"]+)"|value="([^"]+)"[^>]*name="_csrf"`)

// loginAndCrawl logger ind via formular-login og crawler det beskyttede område.
// Best-effort: findes der ingen login-side (GET /login != 200), springes over.
// Fejllinjer markeres med [efter login] så modellen kan skelne dem fra de
// offentlige siders fejl.
func loginAndCrawl(ctx context.Context, base, dir, login string) (failures []string, visited int) {
	user, pass := "admin", "admin"
	if login != "" {
		if u, p, ok := strings.Cut(login, ":"); ok {
			user, pass = u, p
		}
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, 0
	}
	client := &http.Client{Timeout: requestTimeout, Jar: jar}

	resp, err := client.Get(base + "/login")
	if err != nil {
		return nil, 0
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0 // ingen login-side — intet autentificeret flow at teste
	}

	form := url.Values{"username": {user}, "password": {pass}}
	if m := csrfRe.FindStringSubmatch(string(body)); m != nil {
		token := m[1]
		if token == "" {
			token = m[2]
		}
		form.Set("_csrf", token)
	}
	resp2, err := client.PostForm(base+"/login", form)
	if err != nil {
		return []string{"  [efter login] POST /login — netværksfejl: " + err.Error()}, 1
	}
	finalURL := resp2.Request.URL.Path
	resp2.Body.Close()
	if strings.HasPrefix(finalURL, "/login") {
		return []string{fmt.Sprintf("  [efter login] login som %s afvist (endte på %s) — tjek brugeropsætningen i SecurityConfig", user, finalURL)}, 1
	}

	// Crawl autentificeret: start fra landingssiden efter login, alle skannede
	// ruter OG klasse-prefixerne selv (fx /admin) — brugere navigerer dertil,
	// og uden en mapping/redirect rammer de whitelabel efter login.
	seeds := append([]string{finalURL, "/"}, ScanControllerPaths(dir)...)
	seeds = append(seeds, ScanControllerPrefixes(dir)...)
	fails, visited := crawlWithClient(ctx, client, base, seeds)
	for _, f := range fails {
		failures = append(failures, "  [efter login]"+strings.TrimPrefix(f, " "))
	}
	return failures, visited
}

// bodyHint giver et kort, enkelt-linjes uddrag af fejlsidens indhold —
// Whitelabel-sider bærer ofte fejlbeskeden ("There was an unexpected error...").
func bodyHint(body string) string {
	body = strings.Join(strings.Fields(body), " ")
	if body == "" {
		return ""
	}
	if len(body) > 160 {
		body = body[:160] + "…"
	}
	return " — " + body
}

// localIP finder maskinens udadvendte LAN-IP — så brugeren kan tilgå projektet
// fra andre enheder. Falder tilbage til localhost.
func localIP() string {
	conn, err := net.Dial("udp", "192.0.2.1:9") // TEST-NET-1: ingen pakker sendes reelt
	if err != nil {
		return "localhost"
	}
	defer conn.Close()
	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return addr.IP.String()
	}
	return "localhost"
}

func tail(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return "…" + s[len(s)-max:]
}

// boundedBuf gemmer de seneste max bytes — app-output kan være endeløst.
type boundedBuf struct {
	buf []byte
	max int
}

func (b *boundedBuf) Write(p []byte) (int, error) {
	b.buf = append(b.buf, p...)
	if len(b.buf) > b.max {
		b.buf = b.buf[len(b.buf)-b.max:]
	}
	return len(p), nil
}

func (b *boundedBuf) String() string { return string(b.buf) }
