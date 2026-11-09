package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

func (st *SteamTools) getTrendingGames() []map[string]interface{} {
	body, status, err := httpGetBytes("https://store.steampowered.com/api/featuredcategories?cc=us&l=en", 8*time.Second, map[string]string{"User-Agent": "Mozilla/5.0"})
	if err != nil || status != 200 {
		return nil
	}
	var data map[string]interface{}
	if json.Unmarshal(body, &data) != nil {
		return nil
	}
	var games []map[string]interface{}
	seen := map[string]bool{}
	for _, catKey := range []string{"top_sellers", "specials"} {
		cat, _ := data[catKey].(map[string]interface{})
		if cat == nil {
			cat, _ = data["_"+catKey].(map[string]interface{})
		}
		if cat == nil {
			continue
		}
		items := toGameList(cat["items"])
		for i, item := range items {
			if i >= 12 {
				break
			}
			aid := toString(item["id"])
			name := toString(item["name"])
			if aid == "" || name == "" || seen[aid] {
				continue
			}
			seen[aid] = true
			games = append(games, map[string]interface{}{
				"appid": aid, "name": name,
				"_trending_cat": titleCase(strings.ReplaceAll(catKey, "_", " ")),
				"tags":          []interface{}{}, "type": "game", "drm": false, "nsfw": false,
			})
		}
	}
	return games
}

func titleCase(s string) string {
	parts := strings.Split(s, " ")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

func extractHubcapItems(raw interface{}) []map[string]interface{} {
	var items []map[string]interface{}
	switch t := raw.(type) {
	case []interface{}:
		items = toGameList(t)
	case map[string]interface{}:
		for _, key := range []string{"data", "games", "results", "items", "library", "games_list", "entries", "content", "response"} {
			if candidate, ok := t[key].([]interface{}); ok {
				items = toGameList(candidate)
				if len(items) > 0 {
					return items
				}
			}
		}
		if len(items) == 0 {
			for _, v := range t {
				if lst, ok := v.([]interface{}); ok && len(lst) > 0 {
					if m, ok := lst[0].(map[string]interface{}); ok && len(m) > 0 {
						items = toGameList(lst)
						break
					}
				}
			}
		}
	}
	return items
}

func hubcapGameID(g map[string]interface{}) (string, string) {
	gid := toString(g["game_id"])
	if gid == "" {
		gid = toString(g["appid"])
	}
	if gid == "" {
		gid = toString(g["id"])
	}
	if gid == "" {
		gid = toString(g["GameId"])
	}
	if gid == "" {
		gid = toString(g["AppId"])
	}
	name := toString(g["game_name"])
	if name == "" {
		name = toString(g["name"])
	}
	if name == "" {
		name = toString(g["title"])
	}
	if name == "" {
		name = toString(g["gameName"])
	}
	if name == "" {
		name = toString(g["GameName"])
	}
	if name == "" {
		name = toString(g["Title"])
	}
	return gid, name
}

func (st *SteamTools) getAvailableGames(search string, limit int) map[string]interface{} {
	now := nowUnix()
	settings := st.getSettings()
	provider := toString(settings["provider"])
	if provider == "" {
		provider = "default"
	}
	search = strings.ToLower(strings.TrimSpace(search))

	if provider == "hubcap" {
		hubcapKey := strings.TrimSpace(toString(settings["hubcap_api_key"]))
		if hubcapKey != "" {
			if res := st.hubcapAvailable(search, hubcapKey, now); res != nil {
				return res
			}
		}

		st.mu.Lock()
		st.hubcapFallback = true
		st.mu.Unlock()
		provider = "default"
	}

	if provider == "steamtools" {

		if search == "" {
			st.mu.Lock()
			cache := st.gamesCache
			cacheTS := st.gamesCacheTS
			cacheProv := st.gamesCacheProvider
			cacheTotal := st.gamesCacheTotal
			st.mu.Unlock()
			if cache != nil && cacheProv == "steamtools" && now-cacheTS < 600 {
				return map[string]interface{}{"ok": true, "games": cache, "total": cacheTotal, "_trending": true}
			}
		}

		gameCount := 0
		if _, cntData := proxyGameCount(); cntData != nil {
			for _, tk := range []string{"total", "count", "game_count"} {
				if t := toInt(cntData[tk]); t > 0 {
					gameCount = t
					break
				}
			}
			if gameCount == 0 {
				if d, ok := cntData["data"].(map[string]interface{}); ok {
					for _, tk := range []string{"total", "count", "game_count"} {
						if t := toInt(d[tk]); t > 0 {
							gameCount = t
							break
						}
					}
				}
			}
		}
		if search == "" {
			trending := st.getTrendingGames()
			if trending == nil {
				trending = []map[string]interface{}{}
			}
			st.setGamesCacheWithTotal(trending, "steamtools", now, gameCount)
			return map[string]interface{}{"ok": true, "games": trending, "total": gameCount, "_trending": true}
		}
		status, listData := proxySearch("steamtools", search)
		if status != 200 || listData == nil {
			return map[string]interface{}{"ok": false, "games": []interface{}{}, "error": "SteamTools API unreachable via proxy."}
		}
		rawGames := toGameList(listData["games"])
		if rawGames == nil {
			if d, ok := listData["data"]; ok {
				rawGames = toGameList(d)
			}
		}
		if rawGames == nil {
			return map[string]interface{}{"ok": false, "games": []interface{}{}, "error": "Invalid SteamTools API response"}
		}
		var mapped []map[string]interface{}
		for _, g := range rawGames {
			aid := toString(g["appid"])
			name := toString(g["name"])
			if aid == "" || name == "" {
				continue
			}

			gtype := toString(g["type"])
			if gtype == "" {
				gtype = "game"
			}
			if b, _ := g["is_dlc"].(bool); b {
				gtype = "dlc"
			}
			delisted, _ := g["delisted"].(bool)
			nsfw, _ := g["nsfw"].(bool)
			mapped = append(mapped, map[string]interface{}{"appid": aid, "name": name, "tags": []interface{}{}, "type": gtype, "drm": false, "nsfw": nsfw, "delisted": delisted})
		}
		return map[string]interface{}{"ok": true, "games": mapped, "total": gameCount}
	}

	if provider == "depotbox" {
		if search == "" {
			trending := st.getTrendingGames()
			if trending == nil {
				trending = []map[string]interface{}{}
			}
			return map[string]interface{}{"ok": true, "games": trending, "total": len(trending), "_trending": true}
		}
		status, searchData := proxySearch("depotbox", search)
		if status != 200 || searchData == nil {
			return map[string]interface{}{"ok": false, "games": []interface{}{}, "error": "DepotBox API unreachable via proxy."}
		}
		rawGames := toGameList(searchData["games"])
		var mapped []map[string]interface{}
		for _, g := range rawGames {
			aid := toString(g["appid"])
			name := toString(g["name"])
			if aid == "" || name == "" {
				continue
			}
			gtype := "game"
			if b, _ := g["is_dlc"].(bool); b {
				gtype = "dlc"
			}
			mapped = append(mapped, map[string]interface{}{"appid": aid, "name": name, "tags": []interface{}{}, "type": gtype, "drm": false, "nsfw": false})
		}
		return map[string]interface{}{"ok": true, "games": mapped, "total": len(mapped)}
	}

	return st.defaultAvailable(search, limit, now, provider)
}

func (st *SteamTools) hubcapAvailable(search string, key string, now int64) map[string]interface{} {
	const perPage = 5000
	fallback := map[string]interface{}{"ok": false, "games": []interface{}{}}

	st.hubcapMu.Lock()
	partial := append([]map[string]interface{}(nil), st.hubcapCachePartial...)
	partialTotal := st.hubcapTotal
	st.hubcapMu.Unlock()

	st.mu.Lock()
	prov := st.gamesCacheProvider
	st.mu.Unlock()

	if search == "" && prov == "hubcap" && partial != nil {
		st.setGamesCache(partial, "hubcap", now)
		total := partialTotal
		if len(partial) > total {
			total = len(partial)
		}
		return map[string]interface{}{"ok": true, "games": partial, "total": total}
	}

	if search == "" {
		if disk := readHubcapDiskCache(); disk != nil {
			st.hubcapMu.Lock()
			st.hubcapCachePartial = append([]map[string]interface{}(nil), disk...)
			st.hubcapCacheTS = now
			st.hubcapTotal = max(len(disk), partialTotal)
			st.hubcapMu.Unlock()
			st.setGamesCache(disk, "hubcap", now)
			return map[string]interface{}{"ok": true, "games": disk, "total": st.hubcapTotal}
		}
	}

	if search != "" {
		var cachedForSearch []map[string]interface{}
		if prov == "hubcap" && partial != nil {
			cachedForSearch = partial
		} else if disk := readHubcapDiskCache(); disk != nil {
			cachedForSearch = disk
		}
		if cachedForSearch != nil {
			q := search
			var filtered []map[string]interface{}
			for _, g := range cachedForSearch {
				if q != "" && (strings.Contains(strings.ToLower(toString(g["name"])), q) || q == toString(g["appid"])) {
					filtered = append(filtered, g)
				}
			}
			st.setGamesCache(cachedForSearch, "hubcap", now)
			return map[string]interface{}{"ok": true, "games": filtered, "total": len(cachedForSearch)}
		}
	}

	allMapped := []map[string]interface{}{}
	seen := map[string]bool{}
	offset := 0
	total := 0

	for {
		var reqURL string
		if len(search) >= 3 {
			reqURL = fmt.Sprintf("%s/api/v1/search?q=%s&limit=100", HUBCAP_BASE, url.QueryEscape(search))
		} else if search != "" {
			reqURL = fmt.Sprintf("%s/api/v1/library?limit=%d&offset=%d&search=%s", HUBCAP_BASE, perPage, offset, url.QueryEscape(search))
		} else {
			reqURL = fmt.Sprintf("%s/api/v1/library?limit=%d&offset=%d", HUBCAP_BASE, perPage, offset)
		}
		body, status, err := httpGetBytes(reqURL, 30*time.Second, map[string]string{"Authorization": "Bearer " + key, "User-Agent": "Mozilla/5.0"})
		if err != nil {
			return map[string]interface{}{"ok": false, "games": []interface{}{}, "error": "Hubcap API unreachable (" + err.Error() + "). Please verify your API key in Settings."}
		}
		if status != 200 {
			if status == 401 {
				return map[string]interface{}{"ok": false, "games": []interface{}{}, "error": "Hubcap API key is invalid or expired. Please check your API key in Settings."}
			}
			return map[string]interface{}{"ok": false, "games": []interface{}{}, "error": fmt.Sprintf("Hubcap API HTTP %d", status)}
		}
		var raw interface{}
		if json.Unmarshal(body, &raw) != nil {
			return fallback
		}
		items := extractHubcapItems(raw)

		if offset == 0 {
			if m, ok := raw.(map[string]interface{}); ok {
				t := 0
				for _, tk := range []string{"total", "total_count", "count", "found"} {
					if n := toInt(m[tk]); n > 0 {
						t = n
						break
					}
				}
				total = t
			}
		}

		for _, g := range items {
			gid, name := hubcapGameID(g)
			if gid == "" || name == "" || seen[gid] {
				continue
			}
			seen[gid] = true
			tags := []interface{}{}
			for _, tk := range []string{"tags", "genres", "categories", "genre"} {
				if lst, ok := g[tk].([]interface{}); ok {
					tags = lst
					break
				}
			}
			typ := toString(g["type"])
			if typ == "" {
				typ = toString(g["app_type"])
			}
			if typ == "" {
				typ = toString(g["content_type"])
			}
			if typ == "" {
				typ = toString(g["game_type"])
			}
			drm := toBool(g["drm"]) || toBool(g["has_drm"]) || toBool(g["is_drm"]) || toBool(g["drm_protected"])
			nsfw := toBool(g["nsfw"]) || toBool(g["is_nsfw"]) || toBool(g["adult"]) || toBool(g["mature"])
			allMapped = append(allMapped, map[string]interface{}{"appid": gid, "name": name, "tags": tags, "type": typ, "drm": drm, "nsfw": nsfw})
		}

		if (len(search) >= 3) || len(items) == 0 || len(items) < perPage {
			break
		}
		offset += perPage
		if total > 0 && len(allMapped) >= total {
			break
		}
	}

	if total == 0 {
		total = len(allMapped)
	}

	st.hubcapMu.Lock()
	st.hubcapCachePartial = append([]map[string]interface{}(nil), allMapped...)
	st.hubcapTotal = total
	st.hubcapMu.Unlock()
	st.setGamesCache(allMapped, "hubcap", now)

	if search == "" {
		_ = os.MkdirAll(customLocationDir(), 0o755)
		writeJSONMap(hubcapCacheFile(), map[string]interface{}{"games": allMapped, "total": total, "timestamp": now})
	}

	return map[string]interface{}{"ok": true, "games": allMapped, "total": total}
}

func readHubcapDiskCache() []map[string]interface{} {
	data, err := os.ReadFile(hubcapCacheFile())
	if err != nil {
		return nil
	}
	var disk map[string]interface{}
	if json.Unmarshal(data, &disk) != nil {
		return nil
	}
	games, ok := disk["games"].([]interface{})
	if !ok || len(games) == 0 {
		return nil
	}
	ts := toInt64(disk["timestamp"])
	if nowUnix()-ts >= 7*86400 {
		return nil
	}
	return toGameList(games)
}

func (st *SteamTools) setGamesCache(games []map[string]interface{}, provider string, ts int64) {
	st.mu.Lock()
	st.gamesCache = games
	st.gamesCacheTS = ts
	st.gamesCacheProvider = provider
	st.mu.Unlock()
}

func (st *SteamTools) setGamesCacheWithTotal(games []map[string]interface{}, provider string, ts int64, total int) {
	st.mu.Lock()
	st.gamesCache = games
	st.gamesCacheTS = ts
	st.gamesCacheProvider = provider
	st.gamesCacheTotal = total
	st.mu.Unlock()
}

func (st *SteamTools) defaultAvailable(search string, limit int, now int64, provider string) map[string]interface{} {
	st.mu.Lock()
	cache := st.gamesCache
	cacheTS := st.gamesCacheTS
	cacheProv := st.gamesCacheProvider
	st.mu.Unlock()

	if search == "" && cache != nil && cacheProv == provider && now-cacheTS < 600 {
		return map[string]interface{}{"ok": true, "games": cache, "total": len(cache)}
	}

	if search == "" {
		if disk := readGamesDiskCache(); disk != nil {
			st.setGamesCache(disk, provider, now)
			return map[string]interface{}{"ok": true, "games": disk, "total": len(disk)}
		}
	}

	st.mu.Lock()
	gamesURL := st.gamesURL
	st.mu.Unlock()
	if gamesURL == "" {
		cfg := st.loadConfig()
		if ok, _ := cfg["ok"].(bool); !ok {
			return map[string]interface{}{"ok": false, "games": []interface{}{}, "error": "Config not loaded"}
		}
		st.mu.Lock()
		gamesURL = st.gamesURL
		st.mu.Unlock()
		if gamesURL == "" {
			return map[string]interface{}{"ok": false, "games": []interface{}{}, "error": "Config not loaded"}
		}
	}

	body, status, err := httpGetBytes(gamesURL, 8*time.Second, map[string]string{"User-Agent": "Mozilla/5.0"})
	if err != nil || status != 200 {
		if err == nil {
			err = fmt.Errorf("HTTP %d", status)
		}
		return map[string]interface{}{"ok": false, "games": []interface{}{}, "error": err.Error()}
	}
	var games []interface{}
	if json.Unmarshal(body, &games) != nil {
		return map[string]interface{}{"ok": false, "games": []interface{}{}, "error": "Unexpected format"}
	}
	all := toGameList(games)

	var gamesFiltered []map[string]interface{}
	if search != "" {
		q := search
		for _, g := range all {
			if q != "" && (strings.Contains(strings.ToLower(toString(g["name"])), q) || strings.Contains(toString(g["appid"]), q)) {
				gamesFiltered = append(gamesFiltered, g)
			}
		}
	} else if limit > 0 {
		if limit > len(all) {
			limit = len(all)
		}
		gamesFiltered = all[:limit]
	} else {
		gamesFiltered = all
	}

	st.setGamesCache(all, provider, now)

	resp := map[string]interface{}{"ok": true, "games": gamesFiltered, "total": len(all)}
	st.mu.Lock()
	fallbackFlag := st.hubcapFallback
	st.mu.Unlock()
	if fallbackFlag {
		resp["_hubcap_fallback"] = true
		resp["_hubcap_error"] = ""
		st.mu.Lock()
		st.hubcapFallback = false
		st.mu.Unlock()
	}

	if search == "" {
		_ = os.MkdirAll(customLocationDir(), 0o755)
		writeJSONMap(gamesCacheFile(), map[string]interface{}{"games": all, "total": len(all), "timestamp": now})
	}
	return resp
}

func readGamesDiskCache() []map[string]interface{} {
	data, err := os.ReadFile(gamesCacheFile())
	if err != nil {
		return nil
	}
	var disk map[string]interface{}
	if json.Unmarshal(data, &disk) != nil {
		return nil
	}
	games, ok := disk["games"].([]interface{})
	if !ok || len(games) == 0 {
		return nil
	}
	ts := toInt64(disk["timestamp"])
	if nowUnix()-ts >= 7*86400 {
		return nil
	}
	return toGameList(games)
}

func (st *SteamTools) getGamesTotal() map[string]interface{} {
	settings := st.getSettings()
	provider := toString(settings["provider"])
	if provider == "" {
		provider = "default"
	}

	if provider == "hubcap" {
		st.hubcapMu.Lock()
		total := st.hubcapTotal
		partial := len(st.hubcapCachePartial)
		st.hubcapMu.Unlock()
		if partial > total {
			total = partial
		}
		if total > 0 {
			return map[string]interface{}{"total": total}
		}
		key := strings.TrimSpace(toString(settings["hubcap_api_key"]))
		if key != "" {
			body, status, err := httpGetBytes(HUBCAP_BASE+"/api/v1/library?limit=1&offset=0", 8*time.Second, map[string]string{"Authorization": "Bearer " + key, "User-Agent": "Mozilla/5.0"})
			if err == nil && status == 200 {
				var raw map[string]interface{}
				if json.Unmarshal(body, &raw) == nil {
					for _, tk := range []string{"total", "total_count", "count", "found"} {
						if t := toInt(raw[tk]); t > 0 {
							return map[string]interface{}{"total": t}
						}
					}
				}
			}
		}
	}

	if provider == "steamtools" {
		if _, cntData := proxyGameCount(); cntData != nil {
			for _, tk := range []string{"total", "count", "game_count"} {
				if t := toInt(cntData[tk]); t > 0 {
					return map[string]interface{}{"total": t}
				}
			}
			if d, ok := cntData["data"].(map[string]interface{}); ok {
				for _, tk := range []string{"total", "count", "game_count"} {
					if t := toInt(d[tk]); t > 0 {
						return map[string]interface{}{"total": t}
					}
				}
			}
		}
		return map[string]interface{}{"total": 0}
	}

	if provider == "depotbox" {
		body, status, err := httpGetBytes(DEPOTBOX_BASE+"/api/stats", 5*time.Second, map[string]string{"User-Agent": "Mozilla/5.0"})
		if err == nil && status == 200 {
			var raw map[string]interface{}
			if json.Unmarshal(body, &raw) == nil {
				if ok, _ := raw["success"].(bool); ok {
					if d, ok := raw["data"].(map[string]interface{}); ok {
						if t := toInt(d["tracked_games_db1"]); t > 0 {
							return map[string]interface{}{"total": t}
						}
					}
				}
			}
		}
		return map[string]interface{}{"total": 0}
	}

	st.mu.Lock()
	cache := st.gamesCache
	st.mu.Unlock()
	if cache != nil {
		return map[string]interface{}{"total": len(cache)}
	}
	res := st.getAvailableGames("", 0)
	if ok, _ := res["ok"].(bool); ok {
		if t, ok := res["total"].(int); ok && t > 0 {
			return map[string]interface{}{"total": t}
		}
		return map[string]interface{}{"total": len(toGameList(res["games"]))}
	}
	return map[string]interface{}{"total": 0}
}

func toInt(v interface{}) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case string:
		var n int
		fmt.Sscanf(t, "%d", &n)
		return n
	default:
		return 0
	}
}

func toInt64(v interface{}) int64 {
	return int64(toInt(v))
}
