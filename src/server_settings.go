package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// shortDuration formats a duration as a compact string without trailing zero
// components (e.g. 30m instead of 30m0s, 1h instead of 1h0m0s).
func shortDuration(d time.Duration) string {
	if d == 0 {
		return "0"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	switch {
	case h > 0 && m == 0 && s == 0:
		return strconv.Itoa(h) + "h"
	case h > 0 && s == 0:
		return strconv.Itoa(h) + "h" + strconv.Itoa(m) + "m"
	case h == 0 && s == 0:
		return strconv.Itoa(m) + "m"
	default:
		return d.String()
	}
}

func providerDefaults() map[string]string {
	return map[string]string{
		settingProviderOrder: "opensubtitles,subdl,wyzie",
		settingOSEnabled:     "1",
		settingSubDLEnabled:  "1",
		settingWyzieEnabled:  "1",
		settingWorkers:       "5",
		settingAutoScanInterval:  "0",
	}
}

func (s *server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	defaults := providerDefaults()
	for k := range defaults {
		if getSetting(s.db, k) == "" {
			setSetting(s.db, k, defaults[k])
		}
	}

	orderStr := getSetting(s.db, "provider_order")
	if orderStr == "" {
		orderStr = defaults["provider_order"]
	}
	order := strings.Split(orderStr, ",")

	providers := map[string]any{}
	for _, name := range order {
		p := map[string]any{
			"enabled": getSetting(s.db, name+"_enabled") != "0",
		}
		switch name {
		case "opensubtitles":
			p["username"] = getSetting(s.db, settingOSUsername)
			p["password"] = ""
			p["api_key"] = getSetting(s.db, settingOSApiKey)
		case "subdl":
			p["api_key"] = getSetting(s.db, settingSubDLApiKey)
		case "wyzie":
			p["api_key"] = getSetting(s.db, settingWyzieApiKey)
		}
		providers[name] = p
	}
	providers[settingProviderOrder] = order
	workers := int(s.workers.Load())
	providers["workers"] = workers
	s.pollerMu.Lock()
	activeInterval := "0"
	if s.poller != nil {
		// Prefer the DB-stored string (user-facing form like "30m") if set;
		// fall back to formatting the live duration for CLI/env-sourced values.
		if stored := getSetting(s.db, settingAutoScanInterval); stored != "" && stored != "0" {
			activeInterval = stored
		} else {
			activeInterval = shortDuration(s.poller.interval)
		}
	}
	s.pollerMu.Unlock()
	providers["auto_scan_interval"] = activeInterval

	jsonOK(w, providers)
}

func maskOrHidden(s string) string {
	if s == "" {
		return ""
	}
	return maskSecret(s)
}

func (s *server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	var body map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}

	// Workers
	if raw, ok := body["workers"]; ok {
		var n int
		if err := json.Unmarshal(raw, &n); err == nil && n >= 1 && n <= 50 {
			setSetting(s.db, settingWorkers, strconv.Itoa(n))
			s.workers.Store(int32(n))
		}
	}

	// Poll interval
	if raw, ok := body["auto_scan_interval"]; ok {
		var v string
		if err := json.Unmarshal(raw, &v); err == nil {
			if v == "0" || v == "" {
				setSetting(s.db, settingAutoScanInterval, "0")
				s.updatePoller(0)
			} else if d, err := time.ParseDuration(v); err == nil && d >= time.Minute {
				setSetting(s.db, settingAutoScanInterval, v)
				s.updatePoller(d)
			}
		}
	}

	// Provider order
	if raw, ok := body["provider_order"]; ok {
		var order []string
		if err := json.Unmarshal(raw, &order); err == nil {
			setSetting(s.db, settingProviderOrder, strings.Join(order, ","))
		}
	}

	// Per-provider sections
	for _, name := range []string{"opensubtitles", "subdl", "wyzie"} {
		enabledKey := name + "_enabled"
		if raw, ok := body[name]; ok {
			var sec map[string]any
			if err := json.Unmarshal(raw, &sec); err == nil {
				if v, ok := sec["enabled"].(bool); ok {
					if v {
						setSetting(s.db, enabledKey, "1")
					} else {
						setSetting(s.db, enabledKey, "0")
					}
				}
				switch name {
				case "opensubtitles":
					osChanged := false
					if v, ok := sec["openSubtitles_api_key"].(string); ok && v != "" {
						setSetting(s.db, settingOSApiKey, v)
						osChanged = true
					}
					if v, ok := sec["password"].(string); ok && v != "" {
						setSetting(s.db, settingOSPassword, v)
						osChanged = true
					}
					if v, ok := sec["username"].(string); ok && v != "" {
						setSetting(s.db, settingOSUsername, v)
						osChanged = true
					}
					if osChanged {
						s.osProviderMu.Lock()
						if s.osProvider != nil {
							s.osProvider.Logout()
							s.osProvider = nil
						}
						s.osProviderMu.Unlock()
					}
				case "subdl":
					if v, ok := sec["api_key"].(string); ok && v != "" {
						setSetting(s.db, settingSubDLApiKey, v)
					}
				case "wyzie":
					if v, ok := sec["api_key"].(string); ok && v != "" {
						setSetting(s.db, settingWyzieApiKey, v)
					}
				}
			}
		}
	}

	jsonOK(w, map[string]string{"status": "ok"})
}
