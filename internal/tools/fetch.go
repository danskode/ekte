package tools

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// ssrfBlocked returnerer true hvis URL peger på en intern/privat adresse.
// Beskytter mod SSRF når ekte kører i container med adgang til interne netværk.
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
	if ip == nil {
		// Forsøg DNS-opslag for at tjekke om hostnavnet resolver til privat IP.
		// Afvis kun hvis opslaget fejler med en tydelig privat range — ellers lad
		// http.Client håndtere fejlen naturligt.
		addrs, err := net.LookupHost(host)
		if err != nil {
			return false
		}
		if len(addrs) == 0 {
			return false
		}
		ip = net.ParseIP(addrs[0])
		if ip == nil {
			return false
		}
	}
	privateRanges := []string{
		"127.", "::1",
		"10.", "192.168.",
		"169.254.", // cloud metadata (AWS/GCP/Azure)
		"[fc", "[fd", // IPv6 ULA
	}
	s := ip.String()
	for _, prefix := range privateRanges {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	// 172.16.0.0/12
	if strings.HasPrefix(s, "172.") {
		var a, b int
		fmt.Sscanf(s, "172.%d.%d", &a, &b)
		if a >= 16 && a <= 31 {
			return true
		}
	}
	return false
}

// FetchURL henter indhold fra en URL og returnerer ren tekst.
// HTML strippes for tags; andre formater returneres råt (max 1MB).
// Private og cloud-metadata IP-ranges afvises (SSRF-beskyttelse).
func FetchURL(rawURL string) (string, error) {
	if ssrfBlocked(rawURL) {
		return "", fmt.Errorf("URL afvist: peger på privat eller intern adresse (%s)", rawURL)
	}
	client := &http.Client{Timeout: 20 * time.Second}
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
