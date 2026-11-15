//go:build browser

package browsertool

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/EvilFreelancer/coddy-agent/internal/config"
	"github.com/EvilFreelancer/coddy-agent/internal/tooling"
)

// WireSearch installs the browser-backed websearch engine on env. The engine
// uses the same already-authenticated extension connection as the browser
// tool; the extension key never enters a tool call or the model context.
func WireSearch(env *tooling.Env, cfg *config.Config) {
	if env != nil {
		env.BrowserSearch = nil
	}
	if env == nil || cfg == nil || !cfg.Browser.Enable {
		return
	}
	env.BrowserSearch = func(ctx context.Context, query string, page, maxResults int, site string) ([]tooling.BrowserSearchResult, error) {
		return search(ctx, cfg, env, query, page, maxResults, site)
	}
}

type openedTab struct {
	TabID int `json:"tabId"`
}

type searchSnapshot struct {
	Text     string `json:"text"`
	Elements []struct {
		Name string `json:"name"`
		Href string `json:"href"`
	} `json:"elements"`
}

func search(ctx context.Context, cfg *config.Config, env *tooling.Env, query string, page, maxResults int, site string) ([]tooling.BrowserSearchResult, error) {
	bs := backends(cfg)
	name, err := selected(ctx, bs, cfg, env)
	if err != nil {
		return nil, err
	}
	b, id, err := backendOf(bs, name)
	if err != nil {
		return nil, err
	}
	if page < 1 {
		page = 1
	}
	if maxResults < 1 {
		maxResults = 15
	}
	terms := strings.TrimSpace(query)
	if host := strings.TrimSpace(site); host != "" {
		terms += " site:" + host
	}
	u := &url.URL{Scheme: "https", Host: "www.google.com", Path: "/search"}
	values := u.Query()
	values.Set("q", terms)
	values.Set("num", "25")
	if start := (page - 1) * maxResults; start > 0 {
		values.Set("start", itoa(start))
	}
	u.RawQuery = values.Encode()

	call := func(action string, tabID int, params map[string]any) (json.RawMessage, error) {
		raw, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		return pageCall(ctx, b, cfg, env, id, action, tabID, raw)
	}
	raw, err := call("open_tab", 0, map[string]any{"url": u.String()})
	if err != nil {
		return nil, err
	}
	var opened openedTab
	if json.Unmarshal(raw, &opened) != nil || opened.TabID == 0 {
		return nil, errors.New("the browser did not return the search tab")
	}
	defer func() {
		// Closing should still be attempted when the search deadline ended.
		// It is cleanup, not a retry of the action whose outcome was unknown.
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = pageCall(closeCtx, b, cfg, env, id, "close_tab", opened.TabID, json.RawMessage("{}"))
	}()

	raw, err = call("snapshot", opened.TabID, nil)
	if err != nil {
		return nil, err
	}
	var snap searchSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, errors.New("the browser returned an unreadable search page")
	}
	rows := resultsFromSnapshot(snap, maxResults)
	if len(rows) == 0 {
		return nil, errors.New("the browser search page had no readable results; check the tab for a consent or challenge page")
	}
	return rows, nil
}

func pageCall(ctx context.Context, b backend, cfg *config.Config, env *tooling.Env, browserID, action string, tabID int, params json.RawMessage) (json.RawMessage, error) {
	p := page{backend: b, cfg: cfg, env: env, browserID: browserID, action: action, tabID: tabID, params: params}
	out, _, err := p.do(ctx, "", nil)
	return out, err
}

func resultsFromSnapshot(snap searchSnapshot, limit int) []tooling.BrowserSearchResult {
	seen := make(map[string]bool)
	rows := make([]tooling.BrowserSearchResult, 0, limit)
	for _, element := range snap.Elements {
		title := strings.TrimSpace(element.Name)
		href := strings.TrimSpace(element.Href)
		u, err := url.Parse(href)
		if err != nil || title == "" || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
			continue
		}
		host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
		if host == "google.com" || strings.HasSuffix(host, ".google.com") || seen[href] {
			continue
		}
		seen[href] = true
		rows = append(rows, tooling.BrowserSearchResult{Title: title, URL: href})
		if len(rows) >= limit {
			break
		}
	}
	return rows
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
