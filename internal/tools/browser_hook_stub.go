//go:build !browser

package tools

import "github.com/EvilFreelancer/coddy-agent/internal/config"

func registerBrowserTools(_ *Registry, _ *config.Config) {}

func WireBrowserSearch(env *Env, _ *config.Config) {
	if env != nil {
		env.BrowserSearch = nil
	}
}
