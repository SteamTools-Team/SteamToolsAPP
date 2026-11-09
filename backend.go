package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

type SteamTools struct {
	mu sync.Mutex

	hubcapMu sync.Mutex

	push func(map[string]interface{})

	config   map[string]interface{}
	gamesURL string
	apiURL   string
	apiKey   string

	steamCache   map[string]interface{}
	steamCacheTS int64

	gamesCache         []map[string]interface{}
	gamesCacheTS       int64
	gamesCacheProvider string
	gamesCacheTotal    int

	hubcapCachePartial []map[string]interface{}
	hubcapTotal        int
	hubcapCacheTS      int64
	hubcapFallback     bool

	depotboxCache   []map[string]interface{}
	depotboxCacheTS int64

	fixesCache   []map[string]interface{}
	fixesCacheTS int64

	gameFixesCache   []map[string]interface{}
	gameFixesCacheTS int64

	nameResolving bool
}

var api = newSteamTools()

func newSteamTools() *SteamTools {
	return &SteamTools{
		apiKey: "",
	}
}

func (st *SteamTools) setPush(f func(map[string]interface{})) {
	st.mu.Lock()
	st.push = f
	st.mu.Unlock()
}

func (st *SteamTools) cb(kw map[string]interface{}) {
	st.mu.Lock()
	f := st.push
	st.mu.Unlock()
	if f != nil {
		defer func() { _ = recover() }()
		f(kw)
	}
}

func toString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%g", t)
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(t)
	}
}

func toBool(v interface{}) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		return s == "1" || s == "true" || s == "yes" || s == "on"
	case float64:
		return t != 0
	default:
		return false
	}
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func readJSONMap(path string) (map[string]interface{}, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var out map[string]interface{}
	if json.Unmarshal(data, &out) != nil {
		return nil, false
	}
	return out, true
}

func writeJSONMap(path string, m map[string]interface{}) bool {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return false
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return false
	}
	return true
}

func toGameList(v interface{}) []map[string]interface{} {
	switch t := v.(type) {
	case []map[string]interface{}:
		return t
	case []interface{}:
		var out []map[string]interface{}
		for _, it := range t {
			if m, ok := it.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func defaultSettings() map[string]interface{} {
	return map[string]interface{}{
		"custom_steam_path": "",
		"auto_restart":      false,
		"auto_close_steam":  false,
		"confirm_remove":    true,
		"provider":          "steamtools",
		"hubcap_api_key":    "",
		"animations":        true,
		"compact":           false,
		"image_cache":       true,
		"auto_install_ost":  false,
		"download_queue":    false,
	}
}

func (st *SteamTools) getSettings() map[string]interface{} {
	ensureDirs()
	defaults := defaultSettings()
	data, err := os.ReadFile(settingsFile())
	if err != nil {
		return defaults
	}
	var saved map[string]interface{}
	if json.Unmarshal(data, &saved) != nil || saved == nil {
		return defaults
	}
	for k, v := range saved {
		defaults[k] = v
	}
	return defaults
}

func (st *SteamTools) saveSettings(patch map[string]interface{}) map[string]interface{} {
	ensureDirs()
	settings := st.getSettings()
	for k, v := range patch {
		settings[k] = v
	}
	if _, ok := patch["provider"]; ok {
		st.invalidateGamesCache()
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	if err := os.WriteFile(settingsFile(), data, 0o644); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

func (st *SteamTools) validateSteamPath(raw string) map[string]interface{} {
	path := strings.Trim(strings.Trim(strings.TrimSpace(raw), `"`), `'`)
	path = os.ExpandEnv(path)
	path = filepath.Clean(path)
	if strings.HasSuffix(strings.ToLower(path), "steam.exe") {
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
			path = filepath.Dir(path)
		}
	}
	if path == "" {
		return map[string]interface{}{"ok": false, "path": "", "error": "Empty path"}
	}
	if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
		return map[string]interface{}{"ok": false, "path": path, "error": "Path does not exist: " + path}
	}
	if _, err := os.Stat(filepath.Join(path, "steam.exe")); err != nil {
		return map[string]interface{}{"ok": false, "path": path, "error": "steam.exe not found in: " + path}
	}
	return map[string]interface{}{"ok": true, "path": path, "error": ""}
}

func (st *SteamTools) getSteamStatus() map[string]interface{} {
	now := nowUnix()
	st.mu.Lock()
	cache := st.steamCache
	ts := st.steamCacheTS
	st.mu.Unlock()
	if cache != nil && now-ts < 10 {
		return cache
	}
	settings := st.getSettings()
	if custom := toString(settings["custom_steam_path"]); custom != "" {
		v := st.validateSteamPath(custom)
		if ok, _ := v["ok"].(bool); ok {
			result := map[string]interface{}{"detected": true, "path": v["path"], "running": isSteamRunning(), "source": "custom"}
			st.setSteamCache(result, now)
			return result
		}
	}
	for _, regPath := range []string{`SOFTWARE\WOW6432Node\Valve\Steam`, `SOFTWARE\Valve\Steam`} {
		k, err := registry.OpenKey(registry.LOCAL_MACHINE, regPath, registry.QUERY_VALUE|windows.KEY_WOW64_64KEY)
		if err != nil {
			continue
		}
		val, _, err2 := k.GetStringValue("InstallPath")
		k.Close()
		if err2 != nil {
			continue
		}
		p := strings.TrimSpace(val)
		if p != "" {
			if _, err3 := os.Stat(filepath.Join(p, "steam.exe")); err3 == nil {
				result := map[string]interface{}{"detected": true, "path": p, "running": isSteamRunning(), "source": "registry"}
				st.setSteamCache(result, now)
				return result
			}
		}
	}
	for _, path := range steamPaths {
		if _, err := os.Stat(filepath.Join(path, "steam.exe")); err == nil {
			result := map[string]interface{}{"detected": true, "path": path, "running": isSteamRunning(), "source": "auto"}
			st.setSteamCache(result, now)
			return result
		}
	}
	result := map[string]interface{}{"detected": false, "path": "", "running": false, "source": "none"}
	st.setSteamCache(result, now)
	return result
}

func (st *SteamTools) setSteamCache(result map[string]interface{}, ts int64) {
	st.mu.Lock()
	st.steamCache = result
	st.steamCacheTS = ts
	st.mu.Unlock()
}

func (st *SteamTools) invalidateSteamCache() {
	st.mu.Lock()
	st.steamCache = nil
	st.steamCacheTS = 0
	st.mu.Unlock()
}

func (st *SteamTools) launchSteam() map[string]interface{} {
	settings := st.getSettings()
	customPath := toString(settings["custom_steam_path"])
	candidates := append([]string{}, steamPaths...)
	if customPath != "" {
		candidates = append([]string{customPath}, candidates...)
	}
	launched := false
	for _, path := range candidates {
		exe := filepath.Join(path, "steam.exe")
		if _, err := os.Stat(exe); err == nil {
			if err := launchSteamExe(exe); err != nil {
				return map[string]interface{}{"ok": false, "error": err.Error()}
			}
			launched = true
			break
		}
	}
	if launched {
		st.invalidateSteamCache()
	}
	return map[string]interface{}{"ok": launched}
}

func (st *SteamTools) invalidateGamesCache() {
	st.mu.Lock()
	st.gamesCache = nil
	st.gamesCacheTS = 0
	st.gamesCacheProvider = ""
	st.gamesCacheTotal = 0
	st.fixesCache = nil
	st.fixesCacheTS = 0
	st.gameFixesCache = nil
	st.gameFixesCacheTS = 0
	st.depotboxCache = nil
	st.depotboxCacheTS = 0
	st.hubcapCachePartial = nil
	st.hubcapTotal = 0
	st.hubcapCacheTS = 0
	st.mu.Unlock()
}

func (st *SteamTools) clearGameCaches() map[string]interface{} {
	st.invalidateGamesCache()
	for _, p := range []string{hubcapCacheFile(), depotboxCacheFile(), gamesCacheFile(), fixesCacheFile(), gameFixesCacheFile()} {
		_ = os.Remove(p)
	}
	_ = os.RemoveAll(imageCacheDir())
	return map[string]interface{}{"ok": true}
}

func (st *SteamTools) loadConfig() map[string]interface{} {
	url := fmt.Sprintf("%s?nocache=%d", CONFIG_JSON_URL, 100000+rand.Intn(900000))
	data, err := httpGetJSON(url, 6*time.Second, stdHeaders)
	if err != nil {
		return map[string]interface{}{"ok": false, "local_version": LOCAL_VERSION, "error": err.Error()}
	}
	st.mu.Lock()
	st.config = data
	st.gamesURL = toString(data["gamesjsonurl"])
	st.apiURL = toString(data["manifestapiurl"])
	st.mu.Unlock()
	remoteVersion := toString(data["version"])
	return map[string]interface{}{
		"ok":               true,
		"version":          remoteVersion,
		"local_version":    LOCAL_VERSION,
		"update_available": remoteVersion != "" && remoteVersion != LOCAL_VERSION,
		"site":             toString(data["websiteurl"]),
		"discord":          toString(data["discordurl"]),
		"github":           toString(data["githuburl"]),
		"pseudo":           toString(data["creditpseudo"]),
	}
}

func (st *SteamTools) prepareUpdate() map[string]interface{} {
	exePath, err := os.Executable()
	if err != nil {
		exePath = os.Args[0]
	}
	exeDir := filepath.Dir(exePath)
	tmpPath := exePath + ".new"
	batPath := filepath.Join(exeDir, "_steamtools_update.bat")
	updateURL := "https://raw.githubusercontent.com/YOUR-ORG/YOUR-RELEASE-REPO/main/SteamTools.exe"

	_ = os.Remove(tmpPath)
	status, derr := streamDownload(
		dlSpec{Method: "GET", URL: updateURL, Headers: map[string]string{"User-Agent": "Mozilla/5.0"}},
		tmpPath, nil)
	if derr != nil {
		_ = os.Remove(tmpPath)
		return map[string]interface{}{"ok": false, "error": "Download failed: " + derr.Error()}
	}
	if status != 200 {
		_ = os.Remove(tmpPath)
		return map[string]interface{}{"ok": false, "error": fmt.Sprintf("Download failed (HTTP %d)", status)}
	}
	if !isWindowsExecutable(tmpPath) {
		_ = os.Remove(tmpPath)
		return map[string]interface{}{"ok": false, "error": "Downloaded file is not a valid executable"}
	}

	batLines := []string{
		"@echo off",
		":waitloop",
		"del /f /q \"" + exePath + "\" >nul 2>&1",
		"if exist \"" + exePath + "\" (",
		"  timeout /t 1 /nobreak >nul",
		"  goto waitloop",
		")",
		"move /y \"" + tmpPath + "\" \"" + exePath + "\" >nul",
		"start \"\" \"" + exePath + "\"",
		"del /f /q \"%~f0\"",
	}
	bat := strings.Join(batLines, "\r\n") + "\r\n"
	if err := os.WriteFile(batPath, []byte(bat), 0o644); err != nil {
		_ = os.Remove(tmpPath)
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	cmd := execCmd(batPath, exeDir)
	if cmd == nil {
		return map[string]interface{}{"ok": false, "error": "failed to start update process"}
	}
	return map[string]interface{}{"ok": true}
}

func isWindowsExecutable(path string) bool {
	fi, err := os.Stat(path)
	if err != nil || fi.Size() < 1<<20 {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var sig [2]byte
	if _, err := f.Read(sig[:]); err != nil {
		return false
	}
	return sig[0] == 'M' && sig[1] == 'Z'
}

func (st *SteamTools) getCachedImage(appid string) []byte {
	_ = os.MkdirAll(imageCacheDir(), 0o755)
	cachePath := filepath.Join(imageCacheDir(), appid+".jpg")
	if data, err := os.ReadFile(cachePath); err == nil {
		return data
	}
	urls := []string{
		fmt.Sprintf("https://shared.akamai.steamstatic.com/store_item_assets/steam/apps/%s/header.jpg", appid),
		fmt.Sprintf("https://generator.ryuu.lol/files/images/%s.jpg", appid),
	}
	for _, u := range urls {
		body, status, err := httpGetBytes(u, 15*time.Second, stdHeaders)
		if err != nil || status != 200 {
			continue
		}
		_ = os.WriteFile(cachePath, body, 0o644)
		return body
	}
	return nil
}
