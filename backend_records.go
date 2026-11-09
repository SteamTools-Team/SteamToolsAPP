package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func (st *SteamTools) saveInstallRecords(records map[string]interface{}) {
	ensureDirs()
	data, _ := json.MarshalIndent(map[string]interface{}{"installed": records}, "", "  ")
	_ = os.WriteFile(installRecordFile(), data, 0o644)
}

func (st *SteamTools) getInstalledGames() map[string]interface{} {
	ensureDirs()
	data, err := os.ReadFile(installRecordFile())
	if err != nil {
		return map[string]interface{}{}
	}
	var parsed map[string]interface{}
	if json.Unmarshal(data, &parsed) != nil {
		return map[string]interface{}{}
	}
	records, ok := parsed["installed"]
	if !ok {
		records = parsed
	}
	recMap, ok := records.(map[string]interface{})
	if !ok {
		return map[string]interface{}{}
	}
	changed := false
	var pending []string
	for appid, rawInfo := range recMap {
		info, ok := rawInfo.(map[string]interface{})
		if !ok {
			continue
		}
		if _, hasImg := info["img"]; !hasImg && isDigits(appid) {
			info["img"] = fmt.Sprintf("https://shared.akamai.steamstatic.com/store_item_assets/steam/apps/%s/header.jpg", appid)
			changed = true
		}
		name, _ := info["name"].(string)
		if strings.HasPrefix(name, "Game_") && isDigits(appid) {
			pending = append(pending, appid)
		}
	}
	if changed {
		st.saveInstallRecords(recMap)
	}
	if len(pending) > 0 {
		st.spawnNameResolver(pending)
	}
	return recMap
}

func (st *SteamTools) spawnNameResolver(appids []string) {
	st.mu.Lock()
	if st.nameResolving {
		st.mu.Unlock()
		return
	}
	st.nameResolving = true
	st.mu.Unlock()
	go st.resolveNamesInBG(appids)
}

func (st *SteamTools) resolveNamesInBG(appids []string) {
	defer func() {
		st.mu.Lock()
		st.nameResolving = false
		st.mu.Unlock()
	}()
	for _, appid := range appids {
		resolved := st.fetchGameName(appid)
		if resolved == "" {
			continue
		}
		records := st.getInstalledGames()
		info, ok := records[appid].(map[string]interface{})
		if !ok {
			continue
		}
		changed := false
		name, _ := info["name"].(string)
		if strings.HasPrefix(name, "Game_") {
			info["name"] = resolved
			changed = true
		}
		if _, hasImg := info["img"]; !hasImg && isDigits(appid) {
			info["img"] = fmt.Sprintf("https://shared.akamai.steamstatic.com/store_item_assets/steam/apps/%s/header.jpg", appid)
			changed = true
		}
		if !changed {
			continue
		}
		st.saveInstallRecords(records)
		if cache, ok := readJSONMap(nameCacheFile()); ok {
			cache[appid] = resolved
			writeJSONMap(nameCacheFile(), cache)
		}
	}
}

func (st *SteamTools) removeGame(appid string) map[string]interface{} {
	appidS := strings.TrimSpace(appid)
	records := st.getInstalledGames()
	info, _ := records[appidS].(map[string]interface{})
	if info == nil {
		info = map[string]interface{}{}
	}
	delete(records, appidS)
	data, err := json.MarshalIndent(map[string]interface{}{"installed": records}, "", "  ")
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	ensureDirs()
	if err := os.WriteFile(installRecordFile(), data, 0o644); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}

	steam := st.getSteamStatus()
	if detected, _ := steam["detected"].(bool); detected {
		sp, _ := steam["path"].(string)
		fmt.Println("[RemoveGame] Steam path:", sp)
		var candidates []string
		if lf, ok := info["lua_files"].([]interface{}); ok {
			for _, l := range lf {
				candidates = append(candidates, toString(l))
			}
		}
		candidates = append(candidates, appidS+".lua")
		luaDir := filepath.Join(sp, "config", "stplug-in")
		if entries, err := os.ReadDir(luaDir); err == nil {
			for _, e := range entries {
				n := e.Name()
				if strings.HasSuffix(strings.ToLower(n), ".lua") && strings.TrimSuffix(n, filepath.Ext(n)) == appidS {
					candidates = append(candidates, n)
				}
			}
		}
		seen := map[string]bool{}
		for _, name := range candidates {
			if seen[name] {
				continue
			}
			seen[name] = true
			p := filepath.Join(luaDir, name)
			if err := os.Remove(p); err != nil {
				fmt.Println("[RemoveGame] Error deleting", p, err)
			} else {
				fmt.Println("[RemoveGame] Deleted", p)
			}
		}
		depot := filepath.Join(sp, "config", "depotcache")
		if entries, err := os.ReadDir(depot); err == nil {
			for _, e := range entries {
				n := e.Name()
				if n == appidS+".manifest" || strings.HasPrefix(n, appidS+"_") {
					_ = os.Remove(filepath.Join(depot, n))
				}
			}
		}
		for _, acf := range []string{"appmanifest_" + appidS + ".acf", "appmanifest_" + appidS + ".acf_"} {
			_ = os.Remove(filepath.Join(sp, "steamapps", acf))
		}
		if installdir, _ := info["installdir"].(string); installdir != "" {
			_ = os.RemoveAll(filepath.Join(sp, "steamapps", "common", installdir))
		}
	}
	return map[string]interface{}{"ok": true}
}

func (st *SteamTools) exportGamesJSON() string {
	records := st.getInstalledGames()
	data, _ := json.MarshalIndent(map[string]interface{}{"installed": records}, "", "  ")
	return string(data)
}

func (st *SteamTools) importGamesJSON(jsonStr string) map[string]interface{} {
	var data interface{}
	if json.Unmarshal([]byte(jsonStr), &data) != nil {
		return map[string]interface{}{"ok": false, "error": "Invalid JSON"}
	}
	var records map[string]interface{}
	if m, ok := data.(map[string]interface{}); ok {
		if inst, ok := m["installed"]; ok {
			records, _ = inst.(map[string]interface{})
		} else {
			records = m
		}
	} else {
		return map[string]interface{}{"ok": false, "error": "Invalid format"}
	}
	if records == nil {
		return map[string]interface{}{"ok": false, "error": "Invalid format"}
	}
	var games []map[string]interface{}
	for appid, entry := range records {
		appid = strings.TrimSpace(appid)
		if !isDigits(appid) {
			continue
		}
		name := "Game_" + appid
		if m, ok := entry.(map[string]interface{}); ok {
			if n, ok := m["name"].(string); ok && n != "" {
				name = n
			}
		}
		games = append(games, map[string]interface{}{"appid": appid, "name": name})
	}
	return map[string]interface{}{"ok": true, "games": games}
}

var invalidCharsRE = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

func sanitizeDir(name, appid string) string {
	base := strings.TrimSpace(name)
	base = invalidCharsRE.ReplaceAllString(base, "_")
	if len(base) > 60 {
		base = base[:60]
	}
	base = strings.Trim(base, " ._")
	reserved := map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true}
	for i := 1; i < 10; i++ {
		reserved[fmt.Sprintf("COM%d", i)] = true
		reserved[fmt.Sprintf("LPT%d", i)] = true
	}
	if base == "" || reserved[strings.ToUpper(base)] {
		if appid == "" {
			base = "Game_unknown"
		} else {
			base = "Game_" + appid
		}
	}
	return base
}

func (st *SteamTools) registerGame(appid, name, installdir string, icon string, luaFiles []string) {
	records := st.getInstalledGames()
	record := map[string]interface{}{
		"name":       name,
		"installdir": installdir,
		"timestamp":  nowUnix(),
	}
	if len(luaFiles) > 0 {
		arr := make([]interface{}, len(luaFiles))
		for i, l := range luaFiles {
			arr[i] = l
		}
		record["lua_files"] = arr
	}
	if icon != "" {
		record["img"] = icon
	} else if isDigits(appid) {
		record["img"] = fmt.Sprintf("https://shared.akamai.steamstatic.com/store_item_assets/steam/apps/%s/header.jpg", appid)
	}
	records[appid] = record
	st.saveInstallRecords(records)
}

func createACF(appid, name, steamPath string) string {
	installdir := sanitizeDir(name, appid)
	lastOwner := "0"
	userdata := filepath.Join(steamPath, "userdata")
	if entries, err := os.ReadDir(userdata); err == nil {
		for _, e := range entries {
			if isDigits(e.Name()) {
				lastOwner = e.Name()
				break
			}
		}
	}
	acf := fmt.Sprintf(`"AppState"
{
	"appid"		"%s"
	"Universe"		"1"
	"name"		"%s"
	"StateFlags"		"4"
	"installdir"		"%s"
	"LastUpdated"		"%d"
	"UpdateResult"		"0"
	"SizeOnDisk"		"0"
	"buildid"		"1"
	"LastOwner"		"%s"
	"BytesToDownload"		"0"
	"BytesDownloaded"		"0"
	"AutoUpdateBehavior"		"0"
	"AllowOtherDownloadsWhileRunning"		"0"
	"ScheduledAutoUpdate"		"0"
	"UserConfig"	{}
	"MountedConfig"	{}
}`, appid, name, installdir, nowUnix(), lastOwner)
	acfPath := filepath.Join(steamPath, "steamapps", fmt.Sprintf("appmanifest_%s.acf", appid))
	if err := os.WriteFile(acfPath, []byte(acf), 0o644); err != nil {
		return ""
	}
	return installdir
}

var titleRE = regexp.MustCompile(`(?is)<title>(.*?)</title>`)

var badTitleNames = map[string]bool{
	"welcome to steam": true, "error": true, "not found": true, "page not found": true,
	"forbidden": true, "redirecting": true,
}

func (st *SteamTools) fetchGameName(appid string) string {
	appidS := strings.TrimSpace(appid)
	fmt.Println("[NameLookup] Resolving name for AppID", appidS)
	if cache, ok := readJSONMap(nameCacheFile()); ok {
		if cached, ok := cache[appidS].(string); ok && cached != "" {
			fmt.Println("[NameLookup] Cache hit:", appidS, "->", cached)
			return cached
		}
	}
	if res := st.getAvailableGames("", 0); res != nil {
		if ok, _ := res["ok"].(bool); ok {
			for _, g := range toGameList(res["games"]) {
				if toString(g["appid"]) == appidS {
					name := toString(g["name"])
					if name != "" {
						fmt.Println("[NameLookup] Internal catalog:", appidS, "->", name)
						return name
					}
				}
			}
		}
	}
	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

	url := fmt.Sprintf("https://store.steampowered.com/api/appdetails?appids=%s&cc=us&l=english", appidS)
	if body, status, err := httpGetBytes(url, 8*time.Second, map[string]string{"User-Agent": ua, "Referer": "https://store.steampowered.com/"}); err == nil && status == 200 {
		var data map[string]interface{}
		if json.Unmarshal(body, &data) == nil {
			if entry, ok := data[appidS].(map[string]interface{}); ok {
				if success, _ := entry["success"].(bool); success {
					if d, ok := entry["data"].(map[string]interface{}); ok {
						if name := toString(d["name"]); name != "" {
							fmt.Println("[NameLookup] Steam Store API:", appidS, "->", name)
							return name
						}
					}
				}
			}
		}
	}

	url = fmt.Sprintf("https://steamcommunity.com/app/%s/", appidS)
	if body, status, err := httpGetBytes(url, 8*time.Second, map[string]string{"User-Agent": ua}); err == nil && status == 200 {
		if m := titleRE.FindSubmatch(body); m != nil {
			name := strings.TrimSpace(string(m[1]))
			if idx := strings.Index(name, " :: "); idx >= 0 {
				name = strings.TrimSpace(name[idx+4:])
			}
			for _, sep := range []string{" on Steam", " - Steam", " — Steam", " \u2014 Steam"} {
				if i := strings.LastIndex(name, sep); i >= 0 {
					name = strings.TrimSpace(name[:i])
				}
			}
			lower := strings.ToLower(name)
			if name != "" && !badTitleNames[lower] && !strings.Contains(lower, "steamcommunity") {
				fmt.Println("[NameLookup] Steam Community:", appidS, "->", name)
				return name
			}
		}
	}

	url = fmt.Sprintf("https://steamdb.info/app/%s/", appidS)
	if body, status, err := httpGetBytes(url, 5*time.Second, map[string]string{"User-Agent": ua}); err == nil && status == 200 {
		if m := titleRE.FindSubmatch(body); m != nil {
			name := strings.TrimSpace(string(m[1]))
			for _, sep := range []string{" ·", " -", " —", " \u2014"} {
				if i := strings.LastIndex(name, sep); i >= 0 {
					name = strings.TrimSpace(name[:i])
				}
			}
			lower := strings.ToLower(name)
			if name != "" && !badTitleNames[lower] && !strings.Contains(lower, "steamdb") {
				fmt.Println("[NameLookup] SteamDB:", appidS, "->", name)
				return name
			}
		}
	}
	fmt.Println("[NameLookup] All sources failed for", appidS)
	return ""
}
