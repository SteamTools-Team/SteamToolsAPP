package main

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

var fixesLinkRE = regexp.MustCompile(`(?i)<a\s+href="([^"]+\.(?:rar|zip))">([^<]+)</a>\s+(\d{2}-[A-Z][a-z]{2}-\d{4}\s+\d{2}:\d{2})\s+(\S+)`)
var russianSuffixRE = regexp.MustCompile(`\s*по сети\s*$`)

func (st *SteamTools) getFixesList() map[string]interface{} {
	now := nowUnix()
	var fixes []map[string]interface{}

	st.mu.Lock()
	if st.fixesCache != nil && now-st.fixesCacheTS < 600 {
		fixes = st.fixesCache
	}
	st.mu.Unlock()

	if fixes == nil {
		if disk, ts := readFixesDiskCache(); disk != nil && now-ts < 3*86400 {
			fixes = disk
		}
	}

	if fixes == nil {
		body, status, err := httpGetBytes(FIXES_BASE+"/all/", 30*time.Second, map[string]string{"User-Agent": "Mozilla/5.0"})
		if err != nil || status != 200 {
			if err == nil {
				err = fmt.Errorf("HTTP %d", status)
			}
			return map[string]interface{}{"ok": false, "error": err.Error(), "fixes": []interface{}{}}
		}
		for _, m := range fixesLinkRE.FindAllSubmatch(body, -1) {
			urlPath := string(m[1])
			fullURL := FIXES_BASE + "/all/" + urlPath
			label := string(m[2])
			fixSize := strings.TrimSpace(string(m[4]))
			label = regexp.MustCompile(`\.\.&gt;$`).ReplaceAllString(label, "")
			label = html.UnescapeString(label)
			gameName := label
			if idx := strings.LastIndex(label, " - "); idx >= 0 {
				gameName = strings.TrimSpace(label[:idx])
			}
			fixes = append(fixes, map[string]interface{}{
				"name": gameName, "url": fullURL,
				"filename": pathBase(urlPath), "size": fixSize,
			})
		}
	}

	for _, f := range fixes {
		delete(f, "appid")
	}
	st.resolveFixesAppids(fixes)
	sortFixes(fixes)

	st.mu.Lock()
	st.fixesCache = fixes
	st.fixesCacheTS = now
	st.mu.Unlock()
	_ = os.MkdirAll(customLocationDir(), 0o755)
	writeJSONMap(fixesCacheFile(), map[string]interface{}{"fixes": fixes, "timestamp": now})

	return map[string]interface{}{"ok": true, "fixes": fixes}
}

func pathBase(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	return p[strings.LastIndex(p, "/")+1:]
}

type depotboxGameFixItem struct {
	ID           string   `json:"id"`
	DownloadName string   `json:"downloadName"`
	Filename     string   `json:"filename"`
	Size         string   `json:"size"`
	Badges       []string `json:"badges"`
	Tags         []string `json:"tags"`
}

type depotboxGameItem struct {
	Appid string                `json:"appid"`
	Name  string                `json:"name"`
	Fixes []depotboxGameFixItem `json:"fixes"`
}

type depotboxGameFixesResp struct {
	Success bool               `json:"success"`
	Games   []depotboxGameItem `json:"games"`
}

func (st *SteamTools) getGameFixesList() map[string]interface{} {
	now := nowUnix()
	var fixes []map[string]interface{}

	st.mu.Lock()
	if st.gameFixesCache != nil && now-st.gameFixesCacheTS < 600 {
		fixes = st.gameFixesCache
	}
	st.mu.Unlock()

	if fixes == nil {
		if disk, ts := readGameFixesDiskCache(); disk != nil && now-ts < 3*86400 {
			fixes = disk
		}
	}

	if fixes == nil {

		gh := st.githubFixes()

		var dbx []map[string]interface{}
		if status, fixData := proxyFixes(); status == 200 && fixData != nil {
			baseURL := toString(fixData["base_url"])
			if baseURL == "" {
				baseURL = "https://depotbox.example.com"
			}
			for _, g := range toGameList(fixData["games"]) {
				fixList := toGameList(g["fixes"])
				gameName := toString(g["name"])
				gameAppid := toString(g["appid"])
				for _, fx := range fixList {
					dlName := toString(fx["downloadName"])
					if dlName == "" {
						dlName = toString(fx["filename"])
					}
					dbxBadges := []interface{}{"DepotBoxFix"}
					if arr, ok := fx["badges"].([]interface{}); ok {
						dbxBadges = append(dbxBadges, arr...)
					}
					dbx = append(dbx, map[string]interface{}{
						"name":         gameName,
						"appid":        gameAppid,
						"size":         fx["size"],
						"id":           fx["id"],
						"downloadName": fx["downloadName"],
						"filename":     fx["filename"],
						"badges":       dbxBadges,
						"tags":         fx["tags"],
						"url":          baseURL + "/api/game-fixes/download?id=" + url.QueryEscape(toString(fx["id"])) + "&file=" + url.QueryEscape(dlName),
					})
				}
			}
		}

		fixes = append(gh, dbx...)
		if len(fixes) == 0 {
			return map[string]interface{}{"ok": false, "error": "Game fixes unreachable.", "fixes": []interface{}{}}
		}
		sortFixes(fixes)
	}

	st.mu.Lock()
	st.gameFixesCache = fixes
	st.gameFixesCacheTS = now
	st.mu.Unlock()
	_ = os.MkdirAll(customLocationDir(), 0o755)
	writeJSONMap(gameFixesCacheFile(), map[string]interface{}{"fixes": fixes, "timestamp": now, "v": gameFixesCacheVersion})

	return map[string]interface{}{"ok": true, "fixes": fixes}
}

const gameFixesCacheVersion = 5

func readGameFixesDiskCache() ([]map[string]interface{}, int64) {
	data, err := os.ReadFile(gameFixesCacheFile())
	if err != nil {
		return nil, 0
	}
	var disk map[string]interface{}
	if json.Unmarshal(data, &disk) != nil {
		return nil, 0
	}

	if toInt64(disk["v"]) != gameFixesCacheVersion {
		return nil, 0
	}
	fixes, ok := disk["fixes"].([]interface{})
	if !ok || len(fixes) == 0 {
		return nil, 0
	}
	result := toGameList(fixes)
	const depotboxBase = "https://depotbox.example.com"
	for _, f := range result {
		u := toString(f["url"])
		if u != "" && !strings.HasPrefix(u, "http") {
			f["url"] = depotboxBase + u
		}
	}
	return result, toInt64(disk["timestamp"])
}

func readFixesDiskCache() ([]map[string]interface{}, int64) {
	data, err := os.ReadFile(fixesCacheFile())
	if err != nil {
		return nil, 0
	}
	var disk map[string]interface{}
	if json.Unmarshal(data, &disk) != nil {
		return nil, 0
	}
	fixes, ok := disk["fixes"].([]interface{})
	if !ok || len(fixes) == 0 {
		return nil, 0
	}
	return toGameList(fixes), toInt64(disk["timestamp"])
}

func fixSortKey(f map[string]interface{}) string {
	n := strings.ToLower(toString(f["name"]))
	if n == "" {

		return "￿"
	}
	return n
}

func sortFixes(fixes []map[string]interface{}) {
	for i := 1; i < len(fixes); i++ {
		for j := i; j > 0; j-- {
			if fixSortKey(fixes[j-1]) > fixSortKey(fixes[j]) {
				fixes[j-1], fixes[j] = fixes[j], fixes[j-1]
			} else {
				break
			}
		}
	}
}

func readKnownGameMap() map[string]string {
	known := map[string]string{}
	for _, f := range []string{hubcapCacheFile(), gamesCacheFile()} {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var disk map[string]interface{}
		if json.Unmarshal(data, &disk) != nil {
			continue
		}
		for _, g := range toGameList(disk["games"]) {
			gn := strings.ToLower(strings.TrimSpace(toString(g["name"])))
			aid := toString(g["appid"])
			if aid == "" {
				aid = toString(g["game_id"])
			}
			if aid == "" {
				aid = toString(g["id"])
			}
			if gn != "" && aid != "" {
				if _, exists := known[gn]; !exists || f == hubcapCacheFile() {
					known[gn] = aid
				}
			}
		}
	}
	return known
}

func (st *SteamTools) resolveFixesAppids(fixes []map[string]interface{}) {
	knownMap := readKnownGameMap()

	for _, fix := range fixes {
		if appid := toString(fix["appid"]); appid != "" {
			continue
		}
		fixName := russianSuffixRE.ReplaceAllString(toString(fix["name"]), "")
		fixName = strings.TrimSpace(fixName)
		fixNameLower := strings.ToLower(fixName)
		if fixNameLower == "" {
			continue
		}
		if aid, ok := knownMap[fixNameLower]; ok {
			fix["appid"] = aid
			continue
		}
		for gn, aid := range knownMap {
			if len(fixNameLower) >= 4 && (strings.Contains(fixNameLower, gn) || strings.Contains(gn, fixNameLower)) {
				fix["appid"] = aid
				break
			}
		}
	}

	var unmatched []struct {
		fix  map[string]interface{}
		term string
	}
	for _, fix := range fixes {
		if appid := toString(fix["appid"]); appid != "" {
			continue
		}
		term := russianSuffixRE.ReplaceAllString(toString(fix["name"]), "")
		term = strings.TrimSpace(regexp.MustCompile(`\s*[-–]\s*.*`).ReplaceAllString(term, ""))
		if term != "" {
			unmatched = append(unmatched, struct {
				fix  map[string]interface{}
				term string
			}{fix, term})
		}
	}
	if len(unmatched) == 0 {
		return
	}
	if len(unmatched) > 100 {
		unmatched = unmatched[:100]
	}

	headers := map[string]string{
		"User-Agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Accept-Language": "en-US,en;q=0.5",
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, 10)
	appidRE := regexp.MustCompile(`/app/(\d+)`)
	for _, u := range unmatched {
		wg.Add(1)
		sem <- struct{}{}
		go func(u struct {
			fix  map[string]interface{}
			term string
		}) {
			defer wg.Done()
			defer func() { <-sem }()
			aid := lookupFixAppid(u.term, headers, appidRE)
			if aid != "" {
				u.fix["appid"] = aid
			}
		}(u)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
	}
}

func lookupFixAppid(term string, headers map[string]string, appidRE *regexp.Regexp) string {
	terms := []string{term}
	cleaned := regexp.MustCompile(`[^a-zA-Z0-9\s]`).ReplaceAllString(term, " ")
	cleaned = strings.TrimSpace(cleaned)
	if cleaned != "" && cleaned != term {
		terms = append(terms, cleaned)
	}
	for _, t := range terms {
		u := "https://store.steampowered.com/api/storesearch?term=" + url.QueryEscape(t) + "&cc=US&l=en"
		if body, status, err := httpGetBytes(u, 8*time.Second, headers); err == nil && status == 200 {
			var data map[string]interface{}
			if json.Unmarshal(body, &data) == nil {
				if items, ok := data["items"].([]interface{}); ok && len(items) > 0 {
					if m, ok := items[0].(map[string]interface{}); ok {
						if id := toString(m["id"]); id != "" {
							return id
						}
						if id := toString(m["steam_appid"]); id != "" {
							return id
						}
					}
				}
			}
		}
	}
	u := "https://steamcommunity.com/search/SearchCommunityAjax?text=" + url.QueryEscape(term) + "&filter=games&page=1"
	if body, status, err := httpGetBytes(u, 10*time.Second, headers); err == nil && status == 200 {
		if m := appidRE.FindSubmatch(body); m != nil {
			return string(m[1])
		}
	}
	return ""
}

func (st *SteamTools) installFromManifestUpload(uploaded map[string][]byte) map[string]interface{} {
	tmp, err := os.MkdirTemp("", "steamtools-upload-*")
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	defer os.RemoveAll(tmp)

	if len(uploaded) == 0 {
		return map[string]interface{}{"ok": false, "error": "No files provided"}
	}

	for name, data := range uploaded {
		out := filepath.Join(tmp, filepath.FromSlash(name))
		_ = os.MkdirAll(filepath.Dir(out), 0o755)
		_ = os.WriteFile(out, data, 0o644)
	}

	steam := st.getSteamStatus()
	if ok, _ := steam["detected"].(bool); !ok {
		return map[string]interface{}{"ok": false, "error": "Steam not found. Check Settings."}
	}
	steamPath := toString(steam["path"])

	var manifests, luas, zipFiles []string
	var extraTmp []string
	_ = filepath.Walk(tmp, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		low := strings.ToLower(info.Name())
		switch {
		case strings.HasSuffix(low, ".manifest"):
			manifests = append(manifests, path)
		case strings.HasSuffix(low, ".lua"):
			luas = append(luas, path)
		case strings.HasSuffix(low, ".zip"):
			zipFiles = append(zipFiles, path)
		}
		return nil
	})

	for _, zp := range zipFiles {
		ztmp, err := os.MkdirTemp("", "steamtools-zip-*")
		if err != nil {
			continue
		}
		extraTmp = append(extraTmp, ztmp)
		if err := extractZip(zp, ztmp); err != nil {
			continue
		}
		_ = filepath.Walk(ztmp, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			low := strings.ToLower(info.Name())
			if strings.HasSuffix(low, ".manifest") {
				manifests = append(manifests, path)
			} else if strings.HasSuffix(low, ".lua") {
				luas = append(luas, path)
			}
			return nil
		})
	}

	if len(manifests) == 0 {
		return map[string]interface{}{"ok": false, "error": "No .manifest files found in the uploaded files"}
	}

	primaryAppid := ""
	if len(luas) > 0 {
		if ids := regexp.MustCompile(`(\d+)`).FindStringSubmatch(filepath.Base(luas[0])); len(ids) > 1 {
			primaryAppid = ids[1]
			fmt.Println("[ManifestUpload] Primary AppID from .lua:", primaryAppid)
		}
	}

	validated := map[string]string{}
	resolveAppid := func(candidates []string) (string, string) {
		if primaryAppid != "" {
			name, seen := validated[primaryAppid]
			if !seen {
				name = st.fetchGameName(primaryAppid)
				validated[primaryAppid] = name
			}
			if name != "" {
				fmt.Println("[ManifestUpload] Resolved appid", primaryAppid, "-> '"+name+"' (from .lua)")
				return primaryAppid, name
			}
			if len(candidates) > 0 {
				fmt.Println("[ManifestUpload] .lua appid", primaryAppid, "didn't validate, trying manifest fallback")
			}
		}
		for _, c := range candidates {
			name, seen := validated[c]
			if !seen {
				name = st.fetchGameName(c)
				validated[c] = name
			}
			if name != "" {
				fmt.Println("[ManifestUpload] Resolved appid", c, "-> '"+name+"' (manifest fallback)")
				return c, name
			}
		}
		aid := primaryAppid
		if aid == "" && len(candidates) > 0 {
			aid = candidates[0]
		}
		fmt.Println("[ManifestUpload] No candidate validated, falling back to", aid)
		return aid, validated[aid]
	}

	appidGroups := map[string][]string{}
	appidNames := map[string]string{}
	for _, m := range manifests {
		candidates := regexp.MustCompile(`(\d+)`).FindAllString(filepath.Base(m), -1)
		if len(candidates) == 0 && primaryAppid == "" {
			continue
		}
		aid, name := resolveAppid(candidates)
		if aid == "" {
			continue
		}
		appidGroups[aid] = append(appidGroups[aid], m)
		if _, exists := appidNames[aid]; !exists && name != "" {
			appidNames[aid] = name
		}
	}

	if len(appidGroups) == 0 {
		return map[string]interface{}{"ok": false, "error": "Could not extract AppID from any manifest filename"}
	}

	manifestDest := filepath.Join(steamPath, "config", "depotcache")
	luaDest := filepath.Join(steamPath, "config", "stplug-in")
	_ = os.MkdirAll(manifestDest, 0o755)
	_ = os.MkdirAll(luaDest, 0o755)

	var luaNames []string
	for _, l := range luas {
		base := filepath.Base(l)
		_ = copyFile(l, filepath.Join(luaDest, base))
		luaNames = append(luaNames, base)
	}

	var installed []map[string]interface{}
	for appid, mfiles := range appidGroups {
		for _, m := range mfiles {
			_ = copyFile(m, filepath.Join(manifestDest, filepath.Base(m)))
		}
		name := appidNames[appid]
		if name == "" {
			name = "Game_" + appid
		}
		installdir := sanitizeDir(name, appid)
		st.registerGame(appid, name, installdir, "", luaNames)
		installed = append(installed, map[string]interface{}{"ok": true, "game": name, "appid": appid, "installdir": installdir})
	}

	for _, d := range extraTmp {
		_ = os.RemoveAll(d)
	}

	if len(installed) == 0 {
		return map[string]interface{}{"ok": false, "error": "Failed to install any game manifests"}
	}
	return map[string]interface{}{"ok": true, "installed": installed}
}

func (st *SteamTools) detectFixPath(gameName string, appid string) map[string]interface{} {
	if appid != "" {
		path := st.findGamePathByAppId(appid)
		if path != "" {
			return map[string]interface{}{"ok": true, "found": true, "game_path": path}
		}
	}
	path := st.findGamePath(gameName)
	if path != "" {
		return map[string]interface{}{"ok": true, "found": true, "game_path": path}
	}
	return map[string]interface{}{"ok": true, "found": false, "game_path": nil}
}

func (st *SteamTools) findGamePathByAppId(appid string) string {
	steam := st.getSteamStatus()
	steamPath := ""
	if ok, _ := steam["detected"].(bool); ok {
		steamPath = toString(steam["path"])
	}
	var libraries []string
	if steamPath != "" {
		libraries = st.getSteamLibraries(steamPath)
	}
	installdirRE := regexp.MustCompile(`"installdir"\s+"([^"]+)"`)
	manifestName := "appmanifest_" + appid + ".acf"
	for _, lib := range libraries {
		manifestPath := filepath.Join(lib, "steamapps", manifestName)
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			continue
		}
		mi := installdirRE.FindStringSubmatch(string(data))
		if len(mi) < 2 {
			continue
		}
		candidate := filepath.Join(lib, "steamapps", "common", mi[1])
		if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
			return candidate
		}
	}
	return ""
}

func (st *SteamTools) installFix(fixURL, gamePath string) map[string]interface{} {
	if gamePath == "" {
		return map[string]interface{}{"ok": false, "error": "Invalid game path"}
	}
	if fi, err := os.Stat(gamePath); err != nil || !fi.IsDir() {
		return map[string]interface{}{"ok": false, "error": "Invalid game path"}
	}

	tmp, err := os.MkdirTemp("", "steamtools-fix-*")
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	defer os.RemoveAll(tmp)

	fname := ""
	if u, err := url.Parse(fixURL); err == nil {
		if f := u.Query().Get("file"); f != "" {
			fname = pathBase(f)
		}
	}
	if fname == "" {
		fname = pathBase(fixURL)
	}
	fname = strings.SplitN(fname, "?", 2)[0]
	archivePath := filepath.Join(tmp, fname)

	var spec dlSpec
	if isDirectFixURL(fixURL) {
		spec = dlSpec{Method: "GET", URL: fixURL, Headers: map[string]string{"User-Agent": "Mozilla/5.0"}}
	} else {
		proxyBody := map[string]interface{}{"provider": "depotbox", "action": "fix-download", "url": fixURL}
		proxyJSON, _ := json.Marshal(proxyBody)
		proxyPath := "/api/desktop/proxy"
		headers := map[string]string{"Content-Type": "application/json", "User-Agent": "Mozilla/5.0"}
		if token := sessionToken(); token != "" {
			headers["Authorization"] = "Bearer " + token
		}
		for k, v := range signProxyRequest("POST", proxyPath, string(proxyJSON)) {
			headers[k] = v
		}
		spec = dlSpec{Method: "POST", URL: WEBSITE_BASE + proxyPath, Headers: headers, Data: proxyBody}
	}

	status, dlErr := streamDownload(spec, archivePath, nil)
	if dlErr != nil || status != 200 {
		errMsg := "Download failed via proxy"
		switch status {
		case 401:
			errMsg = "Authentication failed. Please reconnect your account."
		case 403:
			errMsg = "Access denied. Your account may not have permission for this fix."
		case 404:
			errMsg = "Fix file not found on the server."
		case 429:
			errMsg = "Too many requests. Please wait a moment and try again."
		case 0:
			errMsg = "Connection failed. Please check your internet connection."
		default:
			if status > 0 {
				errMsg = fmt.Sprintf("Download failed via proxy (HTTP %d). Please try again.", status)
			}
		}
		return map[string]interface{}{"ok": false, "error": errMsg}
	}

	ext := strings.ToLower(filepath.Ext(fname))
	switch ext {
	case ".zip":
		if err := extractZip(archivePath, gamePath); err != nil {
			return map[string]interface{}{"ok": false, "error": "Extraction failed"}
		}
	case ".rar":
		if err := st.extractRar(archivePath, gamePath); err != nil {
			return map[string]interface{}{"ok": false, "error": err.Error()}
		}
	default:
		return map[string]interface{}{"ok": false, "error": fmt.Sprintf("Unsupported archive format: %s", ext)}
	}

	return map[string]interface{}{"ok": true, "game_path": gamePath}
}

func (st *SteamTools) getUnrar() string {
	for _, p := range []string{`C:\Program Files\WinRAR\UnRAR.exe`, `C:\Program Files (x86)\WinRAR\UnRAR.exe`} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("unrar"); err == nil {
		return p
	}
	if p, err := exec.LookPath("unrar.exe"); err == nil {
		return p
	}
	return ""
}

func (st *SteamTools) extractRar(archivePath, destDir string) error {
	unrar := st.getUnrar()
	if unrar == "" {
		return fmt.Errorf("No RAR extraction tool available. Please install WinRAR or 7-Zip.")
	}
	cmd := exec.Command(unrar, "x", "-y", archivePath, destDir+"\\")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

func normalizeGameName(s string) string {
	s = strings.ToLower(s)
	s = russianSuffixRE.ReplaceAllString(s, "")
	s = regexp.MustCompile(`[\[\]\(\)_\-\.\s]+`).ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)
	return s
}

func gameNameScore(query, candidate string) int {
	q := normalizeGameName(query)
	c := normalizeGameName(candidate)
	if q == "" || c == "" {
		return 0
	}
	if q == c {
		return 100
	}
	if strings.Contains(q, c) || strings.Contains(c, q) {
		minLen, maxLen := len(q), len(c)
		if minLen > maxLen {
			minLen, maxLen = maxLen, minLen
		}
		ratio := minLen * 100 / maxLen
		if ratio >= 70 {
			return 80
		}
		return 60
	}
	qWords := strings.Fields(q)
	cWords := strings.Fields(c)
	matched := 0
	for _, qw := range qWords {
		for _, cw := range cWords {
			if qw == cw || strings.Contains(cw, qw) || strings.Contains(qw, cw) {
				matched++
				break
			}
		}
	}
	if len(qWords) > 0 && matched == len(qWords) {
		return 75
	}
	if matched > 0 && matched*2 >= len(qWords) {
		return 40
	}
	return 0
}

func (st *SteamTools) findGamePath(gameName string) string {
	bestPath := ""
	bestScore := 0

	steam := st.getSteamStatus()
	steamPath := ""
	if ok, _ := steam["detected"].(bool); ok {
		steamPath = toString(steam["path"])
	}
	var libraries []string
	if steamPath != "" {
		libraries = st.getSteamLibraries(steamPath)
	}

	records := st.getInstalledGames()
	for _, rawInfo := range records {
		info, ok := rawInfo.(map[string]interface{})
		if !ok {
			continue
		}
		name := toString(info["name"])
		score := gameNameScore(gameName, name)
		if score <= 0 {
			continue
		}
		installdir := toString(info["installdir"])
		for _, lib := range libraries {
			candidate := filepath.Join(lib, "steamapps", "common", installdir)
			if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
				if score > bestScore {
					bestScore = score
					bestPath = candidate
				}
				if score >= 90 {
					return candidate
				}
			}
		}
	}

	nameRE := regexp.MustCompile(`"name"\s+"([^"]+)"`)
	installdirRE := regexp.MustCompile(`"installdir"\s+"([^"]+)"`)
	for _, lib := range libraries {
		appsDir := filepath.Join(lib, "steamapps")
		if fi, err := os.Stat(appsDir); err != nil || !fi.IsDir() {
			continue
		}
		entries, err := os.ReadDir(appsDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if !strings.HasPrefix(name, "appmanifest_") || !strings.HasSuffix(name, ".acf") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(appsDir, name))
			if err != nil {
				continue
			}
			content := string(data)
			mn := nameRE.FindStringSubmatch(content)
			mi := installdirRE.FindStringSubmatch(content)
			if len(mn) < 2 || len(mi) < 2 {
				continue
			}
			score := gameNameScore(gameName, mn[1])
			if score <= 0 {
				continue
			}
			candidate := filepath.Join(lib, "steamapps", "common", mi[1])
			if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
				if score > bestScore {
					bestScore = score
					bestPath = candidate
				}
				if score >= 90 {
					return candidate
				}
			}
		}
	}

	if bestScore < 70 {
		for _, lib := range libraries {
			commonDir := filepath.Join(lib, "steamapps", "common")
			if fi, err := os.Stat(commonDir); err != nil || !fi.IsDir() {
				continue
			}
			entries, err := os.ReadDir(commonDir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				score := gameNameScore(gameName, e.Name())
				if score > bestScore {
					bestScore = score
					bestPath = filepath.Join(commonDir, e.Name())
				}
			}
		}
	}

	if bestScore >= 40 {
		return bestPath
	}
	return ""
}

func (st *SteamTools) getSteamLibraries(steamPath string) []string {
	libs := []string{steamPath}
	vdf := filepath.Join(steamPath, "steamapps", "libraryfolders.vdf")
	data, err := os.ReadFile(vdf)
	if err != nil {
		return libs
	}
	re := regexp.MustCompile(`"\d+"\s+"(.*)"`)
	for _, line := range strings.Split(string(data), "\n") {
		if m := re.FindStringSubmatch(line); len(m) > 1 {
			p := strings.ReplaceAll(m[1], `\\`, `\`)
			if p != "" {
				libs = append(libs, p)
			}
		}
	}
	return libs
}
