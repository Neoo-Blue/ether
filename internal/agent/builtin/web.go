package builtin

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/braedonsaunders/ether/internal/agent/tools"
	"github.com/braedonsaunders/ether/internal/egress"
	"github.com/braedonsaunders/ether/internal/safefetch"
)

// webClient reaches public web pages only. The SSRF guard refuses loopback,
// LAN and cloud-metadata destinations on every hop, so the model cannot use
// the agent to probe the owner's network.
var webClient = (&safefetch.Guard{}).Client(&http.Client{
	Transport: egress.Transport(egress.CapMetadata),
	Timeout:   20 * time.Second,
})

const (
	webUserAgent = "Mozilla/5.0 (compatible; Ether research)"
	webMaxBytes  = 2 << 20
)

// webTools let the agent research real data sources (channel lists, APIs,
// add-on documentation) instead of inventing placeholders. Results are third
// party text and are marked open-world.
func webTools() []tools.Tool {
	return []tools.Tool{
		tool(tools.Tool{
			Name: "web_search", Title: "Search the web", ReadOnly: true, OpenWorld: true,
			Description: "Search the public web. Returns titles, links and snippets. Use it to find real, legal data sources and their APIs (free live channel services, film archives, public lists) before building a plugin; follow up with web_fetch to read a result.",
			InputSchema: schema(map[string]any{
				"query": map[string]any{"type": "string", "description": "What to search for."},
			}, "query"),
			Execute: func(ctx context.Context, _ tools.Principal, in map[string]any) (tools.Result, error) {
				query, err := reqStr(in, "query", 300)
				if err != nil {
					return tools.Result{}, err
				}
				results, err := webSearch(ctx, query)
				if err != nil {
					return tools.Result{}, err
				}
				return tools.Result{Data: map[string]any{"query": query, "results": results}}, nil
			},
		}),
		tool(tools.Tool{
			Name: "web_fetch", Title: "Read a web page", ReadOnly: true, OpenWorld: true,
			Description: "Fetch a public http(s) URL and return its text: readable page text for HTML, the raw body for JSON, M3U, XML or plain text. Use it to inspect an API response, a playlist or documentation before writing plugin code against it.",
			InputSchema: schema(map[string]any{
				"url":      map[string]any{"type": "string", "description": "Public http or https address."},
				"maxChars": map[string]any{"type": "integer", "minimum": 500, "maximum": 40000, "description": "Longest text to return; default 12000."},
			}, "url"),
			Execute: func(ctx context.Context, _ tools.Principal, in map[string]any) (tools.Result, error) {
				raw, err := reqStr(in, "url", 2048)
				if err != nil {
					return tools.Result{}, err
				}
				limit := 12000
				if v, ok := in["maxChars"].(float64); ok && v >= 500 && v <= 40000 {
					limit = int(v)
				}
				return webFetch(ctx, raw, limit)
			},
		}),
	}
}

func webGet(ctx context.Context, raw string) (*http.Response, []byte, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, nil, tools.Userf("Give a public http or https address.")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, nil, tools.Userf("That address is not valid.")
	}
	req.Header.Set("User-Agent", webUserAgent)
	req.Header.Set("Accept", "text/html,application/json,text/plain,*/*;q=0.5")
	resp, err := webClient.Do(req)
	if err != nil {
		return nil, nil, tools.Userf("Could not reach %s: %s", u.Host, tools.SafeMessage(err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, webMaxBytes))
	if err != nil {
		return nil, nil, tools.Userf("Reading %s failed.", u.Host)
	}
	return resp, body, nil
}

func webFetch(ctx context.Context, raw string, limit int) (tools.Result, error) {
	resp, body, err := webGet(ctx, raw)
	if err != nil {
		return tools.Result{}, err
	}
	ctype, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	text := ""
	switch {
	case strings.Contains(ctype, "html"):
		text = htmlText(body)
	case strings.HasPrefix(ctype, "text/"), strings.Contains(ctype, "json"), strings.Contains(ctype, "xml"),
		strings.Contains(ctype, "mpegurl"), ctype == "" || ctype == "application/octet-stream" && isText(body):
		text = string(body)
	default:
		return tools.Result{}, tools.Userf("That address returned %s, which is not text.", ctype)
	}
	truncated := len(text) > limit
	if truncated {
		text = text[:limit]
	}
	return tools.Result{Data: map[string]any{
		"url": resp.Request.URL.String(), "status": resp.StatusCode, "contentType": ctype,
		"text": text, "truncated": truncated,
	}}, nil
}

func isText(b []byte) bool {
	n := len(b)
	if n > 512 {
		n = 512
	}
	for _, c := range b[:n] {
		if c == 0 {
			return false
		}
	}
	return true
}

// htmlText keeps the readable text of a page and its link targets.
func htmlText(body []byte) string {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return ""
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "noscript", "svg", "head":
				return
			}
		}
		if n.Type == html.TextNode {
			if s := strings.Join(strings.Fields(n.Data), " "); s != "" {
				b.WriteString(s)
				b.WriteByte(' ')
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if n.Type == html.ElementNode {
			switch n.Data {
			case "a":
				for _, a := range n.Attr {
					if a.Key == "href" && strings.HasPrefix(a.Val, "http") {
						fmt.Fprintf(&b, "(%s) ", a.Val)
					}
				}
			case "p", "div", "li", "tr", "h1", "h2", "h3", "h4", "br", "section", "article":
				b.WriteByte('\n')
			}
		}
	}
	walk(doc)
	return strings.TrimSpace(b.String())
}

// webSearch uses DuckDuckGo's plain HTML results page: no key, no account.
func webSearch(ctx context.Context, query string) ([]map[string]string, error) {
	resp, body, err := webGet(ctx, "https://html.duckduckgo.com/html/?q="+url.QueryEscape(query))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, tools.Userf("Web search answered HTTP %d.", resp.StatusCode)
	}
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, tools.Userf("Web search returned an unreadable page.")
	}
	var out []map[string]string
	var cur map[string]string
	var walk func(*html.Node)
	text := func(n *html.Node) string {
		var b strings.Builder
		var f func(*html.Node)
		f = func(n *html.Node) {
			if n.Type == html.TextNode {
				b.WriteString(n.Data)
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				f(c)
			}
		}
		f(n)
		return strings.Join(strings.Fields(b.String()), " ")
	}
	class := func(n *html.Node) string {
		for _, a := range n.Attr {
			if a.Key == "class" {
				return a.Val
			}
		}
		return ""
	}
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" && strings.Contains(class(n), "result__a") {
			href := ""
			for _, a := range n.Attr {
				if a.Key == "href" {
					href = a.Val
				}
			}
			// Result links are redirects carrying the target in uddg.
			if u, err := url.Parse(href); err == nil {
				if target := u.Query().Get("uddg"); target != "" {
					href = target
				}
			}
			cur = map[string]string{"title": text(n), "url": href}
			out = append(out, cur)
		}
		if n.Type == html.ElementNode && strings.Contains(class(n), "result__snippet") && cur != nil {
			cur["snippet"] = truncate(text(n), 300)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	if len(out) > 8 {
		out = out[:8]
	}
	if len(out) == 0 {
		return nil, tools.Userf("No web results for that search.")
	}
	return out, nil
}
