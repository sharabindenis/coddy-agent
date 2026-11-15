//go:build browser

package browsertool

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/EvilFreelancer/coddy-agent/external/browser"
	"github.com/EvilFreelancer/coddy-agent/external/browser/browsertest"
	"github.com/EvilFreelancer/coddy-agent/internal/config"
)

func TestBrowserSearchUsesAuthenticatedConnectionAndClosesItsTab(t *testing.T) {
	mux := http.NewServeMux()
	mounted := browser.Mount(mux, browser.MountOptions{Key: func() string { return testKey }})
	srv := httptest.NewServer(mux)
	ext, err := browsertest.Connect(srv.URL, browsertest.Options{Key: testKey, ID: "browser-search-0001"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = ext.Close()
		srv.Close()
		mounted.Close()
	})
	if err := waitFor(func() bool { return browser.Default().Connected("browser-search-0001") }); err != nil {
		t.Fatal(err)
	}
	env := chat(t)
	cfg := &config.Config{}
	cfg.Browser.Enable = true
	WireSearch(env, cfg)
	if env.BrowserSearch == nil {
		t.Fatal("browser search was not wired")
	}
	const tabID = 77
	var actions []string
	ext.Handle = func(c browsertest.Command) browsertest.Answer {
		actions = append(actions, c.Action)
		switch c.Action {
		case "open_tab":
			var p struct {
				URL string `json:"url"`
			}
			_ = json.Unmarshal(c.Params, &p)
			if !strings.Contains(p.URL, "google.com/search") || !strings.Contains(p.URL, "site%3Ago.dev") {
				return browsertest.Answer{Error: "unexpected search URL: " + p.URL}
			}
			if err := ext.Publish(browsertest.Tab{ID: tabID, URL: p.URL, Title: "Search", SessionID: c.Session.ID}); err != nil {
				return browsertest.Answer{Error: err.Error()}
			}
			return browsertest.Answer{Result: map[string]any{"tabId": tabID, "url": p.URL}}
		case "snapshot":
			return browsertest.Answer{Result: map[string]any{"elements": []map[string]string{
				{"name": "Google navigation", "href": "https://www.google.com/preferences"},
				{"name": "Context package", "href": "https://pkg.go.dev/context"},
			}}}
		case "close_tab":
			_ = ext.Publish()
			return browsertest.Answer{Result: map[string]bool{"ok": true}}
		default:
			return browsertest.Answer{Error: "unexpected action: " + c.Action}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := env.BrowserSearch(ctx, "golang context", 1, 10, "go.dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].URL != "https://pkg.go.dev/context" {
		t.Fatalf("rows = %+v", rows)
	}
	if got := strings.Join(actions, ","); got != "open_tab,snapshot,close_tab" {
		t.Fatalf("actions = %s", got)
	}
}

func TestWireSearchClearsCapabilityWhenBrowserIsDisabled(t *testing.T) {
	env := chat(t)
	cfg := &config.Config{}
	cfg.Browser.Enable = true
	WireSearch(env, cfg)
	cfg.Browser.Enable = false
	WireSearch(env, cfg)
	if env.BrowserSearch != nil {
		t.Fatal("browser search remained wired after browser.enable was turned off")
	}
}
