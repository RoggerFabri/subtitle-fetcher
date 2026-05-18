package main

import (
	"fmt"
	"net/http"
	"strings"
)

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	defaults := providerDefaults()
	for k := range defaults {
		if getSetting(s.db, k) == "" {
			setSetting(s.db, k, defaults[k])
		}
	}

	orderStr := getSetting(s.db, settingProviderOrder)
	if orderStr == "" {
		orderStr = defaults[settingProviderOrder]
	}
	order := strings.Split(orderStr, ",")

	providers := map[string]any{}
	for _, name := range order {
		enabled := getSetting(s.db, name+"_enabled") != "0"
		p := map[string]any{
			"configured": false,
			"enabled":    enabled,
		}
		switch name {
		case "opensubtitles":
			u := getSetting(s.db, settingOSUsername)
			pw := getSetting(s.db, settingOSPassword)
			k := getSetting(s.db, settingOSApiKey)
			p["configured"] = u != "" && pw != "" && k != ""
			p["username"] = u
		case "subdl":
			k := getSetting(s.db, settingSubDLApiKey)
			p["configured"] = k != ""
		case "wyzie":
			p["configured"] = getSetting(s.db, settingWyzieApiKey) != ""
		}
		providers[name] = p
	}

	jsonOK(w, map[string]any{
		"providers":      providers,
		"provider_order": order,
	})
}

func (s *server) handleTestConnection(w http.ResponseWriter, r *http.Request) {
	providers, err := s.loadProviders()
	if err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if len(providers) == 0 {
		jsonOK(w, map[string]any{"ok": false, "error": "no providers configured"})
		return
	}
	for _, p := range providers {
		if p.Name() == "opensubtitles" {
			if err := p.Open(); err != nil {
				jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			defer p.Close()
			cl, err2 := s.makeClient()
			if err2 != nil {
				jsonOK(w, map[string]any{"ok": false, "error": err2.Error()})
				return
			}
			defer cl.logout()
			info, err3 := cl.getUser()
			if err3 != nil {
				jsonOK(w, map[string]any{"ok": false, "error": err3.Error()})
				return
			}
			jsonOK(w, map[string]any{
				"ok":                  true,
				"level":               info.Level,
				"remaining_downloads": info.RemainingDownloads,
				"allowed_downloads":   info.AllowedDownloads,
				"vip":                 info.VIP,
			})
			return
		}
	}
	jsonOK(w, map[string]any{"ok": false, "error": "opensubtitles not configured"})
}

func (s *server) handleTestProvider(w http.ResponseWriter, r *http.Request) {
	providerName := r.URL.Query().Get("provider")
	if providerName == "" {
		jsonError(w, "missing provider param", http.StatusBadRequest)
		return
	}

	switch providerName {
	case "opensubtitles":
		cl, err := s.makeClient()
		if err != nil {
			jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		defer cl.logout()
		info, err := cl.getUser()
		if err != nil {
			jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		jsonOK(w, map[string]any{
			"ok":                  true,
			"level":               info.Level,
			"remaining_downloads": info.RemainingDownloads,
			"allowed_downloads":   info.AllowedDownloads,
			"vip":                 info.VIP,
		})
	case "subdl":
		k := getSetting(s.db, settingSubDLApiKey)
		if k == "" {
			jsonOK(w, map[string]any{"ok": false, "error": "API key not configured"})
			return
		}
		p := newSubDLProvider(k)
		res, err := p.search(map[string]string{
			"api_key":       k,
			"languages":     "EN",
			"type":          "movie",
			"film_name":     "test",
			"subs_per_page": "1",
		})
		if err != nil {
			jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		jsonOK(w, map[string]any{
			"ok":      true,
			"results": len(res),
		})
	case "wyzie":
		k := getSetting(s.db, settingWyzieApiKey)
		if k == "" {
			jsonOK(w, map[string]any{"ok": false, "error": "API key not configured"})
			return
		}
		p := newWyzieProvider(k)
		res, err := p.search("0816692", 0, 0, false) // Interstellar
		if err != nil {
			jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		jsonOK(w, map[string]any{"ok": true, "results": len(res)})
	default:
		jsonError(w, "unknown provider: "+providerName, http.StatusBadRequest)
	}
}

func (s *server) loadProviders() ([]subtitleProvider, error) {
	defaults := providerDefaults()
	for k := range defaults {
		if getSetting(s.db, k) == "" {
			setSetting(s.db, k, defaults[k])
		}
	}

	order := getSetting(s.db, settingProviderOrder)
	if order == "" {
		order = defaults[settingProviderOrder]
	}

	var out []subtitleProvider
	for _, name := range strings.Split(order, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if getSetting(s.db, name+"_enabled") == "0" {
			continue
		}
		switch name {
		case "opensubtitles":
			u, pw, k := getSetting(s.db, settingOSUsername),
				getSetting(s.db, settingOSPassword),
				getSetting(s.db, settingOSApiKey)
			if u != "" && pw != "" && k != "" {
				s.osProviderMu.Lock()
				if s.osProvider == nil {
					s.osProvider = newOpenSubtitlesProvider(u, pw, k)
				}
				p := s.osProvider
				s.osProviderMu.Unlock()
				out = append(out, p)
			}
		case "subdl":
			if k := getSetting(s.db, settingSubDLApiKey); k != "" {
				out = append(out, newSubDLProvider(k))
			}
		case "wyzie":
			if k := getSetting(s.db, settingWyzieApiKey); k != "" {
				out = append(out, newWyzieProvider(k))
			}
		}
	}
	return out, nil
}

// makeClient reads the OpenSubtitles prefixed keys.
func (s *server) makeClient() (*client, error) {
	username := getSetting(s.db, settingOSUsername)
	password := getSetting(s.db, settingOSPassword)
	apiKey := getSetting(s.db, settingOSApiKey)
	if username == "" || password == "" || apiKey == "" {
		return nil, fmt.Errorf("credentials not configured — open Settings")
	}
	cl := newClient(apiKey, username, password)
	if err := cl.login(); err != nil {
		return nil, err
	}
	return cl, nil
}
