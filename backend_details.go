package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

type detailsEntry struct {
	ts  int64
	res map[string]interface{}
}

var (
	detailsMu    sync.Mutex
	detailsCache = map[string]detailsEntry{}
)

var (
	tagOpenRE  = regexp.MustCompile(`(?is)<script.*?</script>`)
	tagCloseRE = regexp.MustCompile(`(?i)\s+on\w+\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`)
	jsURLRE    = regexp.MustCompile(`(?i)\b(javascript|vbscript):`)
)

func sanitizeSteamHTML(s string) string {
	s = tagOpenRE.ReplaceAllString(s, "")
	s = tagCloseRE.ReplaceAllString(s, "")
	s = jsURLRE.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

func apiURL(appid string) string {
	return fmt.Sprintf("https://store.steampowered.com/api/appdetails?appids=%s&cc=us&l=english", appid)
}

func steamDetailsUA() map[string]string {
	return map[string]string{
		"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Referer":    "https://store.steampowered.com/",
		"Accept":     "application/json",
	}
}

func stringList(v interface{}) []string {
	out := []string{}
	switch t := v.(type) {
	case []interface{}:
		for _, it := range t {
			if s := toString(it); s != "" {
				out = append(out, s)
			}
		}
	case []string:
		out = append(out, t...)
	}
	return out
}

func objectStrings(v interface{}, key string) []string {
	out := []string{}
	list, ok := v.([]interface{})
	if !ok {
		return out
	}
	for _, it := range list {
		if m, ok := it.(map[string]interface{}); ok {
			if s := toString(m[key]); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func firstString(v interface{}) string {
	if s := toString(v); s != "" {
		return s
	}
	return ""
}

func mergeGameMeta(details map[string]interface{}, catalog []map[string]interface{}, appid string) {
	for _, g := range catalog {
		if toString(g["appid"]) != appid {
			continue
		}
		if name := toString(g["name"]); name != "" && toString(details["name"]) == "" {
			details["name"] = name
		}
		details["drm"] = toBool(g["drm"])
		details["nsfw"] = toBool(g["nsfw"])
		if typ := toString(g["type"]); typ != "" {
			details["type"] = typ
		}
		if tags, ok := g["tags"].([]interface{}); ok {
			details["tags"] = tags
		}
		break
	}
}

func (st *SteamTools) getGameDetails(appid string) map[string]interface{} {
	appid = strings.TrimSpace(appid)
	if appid == "" {
		return map[string]interface{}{"ok": false, "error": "Missing appid"}
	}

	now := nowUnix()
	detailsMu.Lock()
	if e, ok := detailsCache[appid]; ok && now-e.ts < 3600 {
		detailsMu.Unlock()
		return e.res
	}
	detailsMu.Unlock()

	var catalog []map[string]interface{}
	if res := st.getAvailableGames("", 0); res != nil {
		catalog = toGameList(res["games"])
	}

	base := map[string]interface{}{
		"appid": appid,
		"ok":    false,
	}
	mergeGameMeta(base, catalog, appid)

	body, status, err := httpGetBytes(apiURL(appid), 12*time.Second, steamDetailsUA())
	if err != nil || status != 200 {
		if err == nil {
			err = fmt.Errorf("Steam Store API HTTP %d", status)
		}
		base["error"] = "Steam Store API unreachable: " + err.Error()
		return base
	}

	var raw map[string]interface{}
	if json.Unmarshal(body, &raw) != nil {
		base["error"] = "Invalid Steam Store response"
		return base
	}

	entry, ok := raw[appid].(map[string]interface{})
	if !ok || !toBool(entry["success"]) {
		base["error"] = "Game not found on Steam Store"
		return base
	}
	d, ok := entry["data"].(map[string]interface{})
	if !ok {
		base["error"] = "Steam Store returned no data"
		return base
	}

	details := map[string]interface{}{
		"ok":                true,
		"appid":             toString(d["steam_appid"]),
		"name":              toString(d["name"]),
		"short_description": toString(d["short_description"]),
		"about":             sanitizeSteamHTML(toString(d["about_the_game"])),
		"detailed":          sanitizeSteamHTML(toString(d["detailed_description"])),
		"developers":        stringList(d["developers"]),
		"publishers":        stringList(d["publishers"]),
		"genres":            objectStrings(d["genres"], "description"),
		"categories":        objectStrings(d["categories"], "description"),
		"website":           toString(d["website"]),
		"header_image":      toString(d["header_image"]),
		"capsule_image":     toString(d["capsule_image"]),
		"library_hero":      toString(d["library_hero"]),
		"background":        toString(d["background"]),
		"background_raw":    toString(d["background_raw"]),
		"type":              toString(d["type"]),
		"is_free":           toBool(d["is_free"]),
		"required_age":      toInt(d["required_age"]),
		"store_url":         fmt.Sprintf("https://store.steampowered.com/app/%s/", appid),
		"drm":               toBool(d["drm"]),
		"nsfw":              toBool(d["content_descriptors"] != nil && isAdultDescriptor(d)),
		"platforms":         platformMap(d["platforms"]),
		"price":             priceMap(d["price_overview"]),
		"release_date":      dateMap(d["release_date"]),
		"metacritic":        scoreValue(d["metacritic"]),
	}

	if rec, ok := d["recommendations"].(map[string]interface{}); ok {
		details["recommendations"] = toInt(rec["total"])
	}

	if po, ok := d["price_overview"].(map[string]interface{}); ok {
		if disc := toInt(po["discount_percent"]); disc > 0 {
			details["discount"] = disc
		}
	}

	if pc, ok := d["pc_requirements"].(map[string]interface{}); ok {
		details["pc_requirements"] = sanitizeSteamHTML(toString(pc["minimum"]))
	}

	screenshots := []map[string]interface{}{}
	if sh, ok := d["screenshots"].([]interface{}); ok {
		for _, s := range sh {
			if sm, ok := s.(map[string]interface{}); ok {
				screenshots = append(screenshots, map[string]interface{}{
					"thumb": toString(sm["path_thumbnail"]),
					"full":  toString(sm["path_full"]),
				})
			}
		}
	}
	details["screenshots"] = screenshots

	movies := []map[string]interface{}{}
	if mv, ok := d["movies"].([]interface{}); ok {
		for _, s := range mv {
			if sm, ok := s.(map[string]interface{}); ok {
				movie := map[string]interface{}{
					"name":  toString(sm["name"]),
					"thumb": toString(sm["thumbnail"]),
					"mp4":   firstMapStr(sm, "mp4", "480"),
				}
				if m, ok := sm["mp4"].(map[string]interface{}); ok {
					if url := firstString(m["max"]); url != "" {
						movie["mp4"] = url
					} else if url := firstString(m["480"]); url != "" {
						movie["mp4"] = url
					}
				}
				movies = append(movies, movie)
			}
		}
	}
	details["movies"] = movies

	mergeGameMeta(details, catalog, appid)

	detailsMu.Lock()
	detailsCache[appid] = detailsEntry{ts: now, res: details}
	detailsMu.Unlock()

	return details
}

func isAdultDescriptor(d map[string]interface{}) bool {
	cd, ok := d["content_descriptors"].(map[string]interface{})
	if !ok {
		return false
	}
	ids, _ := cd["ids"].([]interface{})
	for _, id := range ids {
		if toInt(id) >= 2 {
			return true
		}
	}
	return false
}

func platformMap(v interface{}) []string {
	m, ok := v.(map[string]interface{})
	if !ok {
		return []string{}
	}
	out := []string{}
	for _, p := range []string{"windows", "mac", "linux"} {
		if toBool(m[p]) {
			out = append(out, p)
		}
	}
	return out
}

func priceMap(v interface{}) map[string]interface{} {
	p, ok := v.(map[string]interface{})
	if !ok {
		return map[string]interface{}{}
	}
	return map[string]interface{}{
		"currency":          toString(p["currency"]),
		"initial":           toInt(p["initial"]),
		"final":             toInt(p["final"]),
		"discount_percent":  toInt(p["discount_percent"]),
		"initial_formatted": toString(p["initial_formatted"]),
		"final_formatted":   toString(p["final_formatted"]),
	}
}

func dateMap(v interface{}) map[string]interface{} {
	d, ok := v.(map[string]interface{})
	if !ok {
		return map[string]interface{}{}
	}
	return map[string]interface{}{
		"date":        toString(d["date"]),
		"coming_soon": toBool(d["coming_soon"]),
	}
}

func scoreValue(v interface{}) map[string]interface{} {
	sc, ok := v.(map[string]interface{})
	if !ok {
		return map[string]interface{}{}
	}
	return map[string]interface{}{"score": toInt(sc["score"]), "url": toString(sc["url"])}
}

func firstMapStr(m map[string]interface{}, key string, subkeys ...string) string {
	for _, sub := range subkeys {
		if v, ok := m[sub].(string); ok && v != "" {
			return v
		}
	}
	return ""
}
