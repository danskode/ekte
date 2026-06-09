package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// isPrivateIP returnerer true for loopback, link-local, private og ULA-adresser.
// Bruger Go 1.17+ ip.IsPrivate() der korrekt håndterer IPv6 ULA (fc00::/7).
func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if ip.IsPrivate() { // dækker 10/8, 172.16/12, 192.168/16, fc00::/7
		return true
	}
	// cloud metadata: 169.254.169.254 (allerede dækket af IsLinkLocalUnicast,
	// men eksplicit for klarhed)
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
	}
	return false
}

// ssrfBlocked laver en hurtig check inden forbindelsen oprettes — afviser
// literale private IP-adresser og kendte loopback-navne uden DNS-opslag.
func ssrfBlocked(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" || host == "ip6-localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip != nil {
		return isPrivateIP(ip)
	}
	return false
}

// newSafeHTTPClient returnerer en http.Client med en DialContext-hook der
// resolver DNS og validerer den opnåede IP inden forbindelsen etableres.
// Dette eliminerer SSRF TOCTOU-vinduet: DNS resolver kun ét sted.
func newSafeHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			// Resolver DNS eksplicit — vi kontrollerer hvilken IP der forbindes til.
			addrs, err := net.DefaultResolver.LookupHost(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, a := range addrs {
				ip := net.ParseIP(a)
				if ip != nil && isPrivateIP(ip) {
					return nil, fmt.Errorf("SSRF-beskyttelse: %s resolver til privat adresse %s", host, a)
				}
			}
			if len(addrs) == 0 {
				return nil, fmt.Errorf("DNS-opslag gav ingen resultater for %s", host)
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(addrs[0], port))
		},
	}
	return &http.Client{
		Timeout:   20 * time.Second,
		Transport: transport,
	}
}

// FetchURL henter indhold fra en URL og returnerer ren tekst.
// HTML strippes for tags; andre formater returneres råt (max 1MB).
// Private og cloud-metadata IP-ranges afvises (SSRF-beskyttelse).
func FetchURL(rawURL string) (string, error) {
	if ssrfBlocked(rawURL) {
		return "", fmt.Errorf("URL afvist: peger på privat eller intern adresse (%s)", rawURL)
	}
	client := newSafeHTTPClient()
	resp, err := client.Get(rawURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}

	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "text/html") {
		return extractHTMLText(body), nil
	}
	text := string(body)
	if len(text) > 12000 {
		text = text[:12000] + "\n[...]"
	}
	return text, nil
}

var skipTags = map[string]bool{
	"script": true, "style": true, "nav": true,
	"footer": true, "head": true, "form": true,
	"aside": true, "button": true,
}

func extractHTMLText(b []byte) string {
	doc, err := html.Parse(bytes.NewReader(b))
	if err != nil {
		s := string(b)
		if len(s) > 12000 {
			s = s[:12000] + "\n[...]"
		}
		return s
	}
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && skipTags[n.Data] {
			return
		}
		if n.Type == html.TextNode {
			t := strings.TrimSpace(n.Data)
			if t != "" {
				sb.WriteString(t + "\n")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	result := sb.String()
	if len(result) > 12000 {
		result = result[:12000] + "\n[...]"
	}
	return result
}
