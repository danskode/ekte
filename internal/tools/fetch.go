package tools

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// FetchURL henter indhold fra en URL og returnerer ren tekst.
// HTML strippes for tags; andre formater returneres råt (max 1MB).
func FetchURL(rawURL string) (string, error) {
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
