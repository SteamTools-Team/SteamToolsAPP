package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	samClientVersion = "SteamClient018"
	samUserVersion   = "SteamUser012"
	samStatsVersion  = "STEAMUSERSTATS_INTERFACE_VERSION013"
	samAppsVersion   = "STEAMAPPS_INTERFACE_VERSION008"

	vtClientCreateSteamPipe     = 0
	vtClientReleaseSteamPipe    = 1
	vtClientConnectToGlobalUser = 2
	vtClientGetISteamUser       = 5
	vtClientGetISteamUserStats  = 13
	vtClientGetISteamApps       = 15

	vtUserGetSteamID = 2

	vtAppsIsSubscribedApp = 6

	vtStatsGetAchievement                 = 5
	vtStatsSetAchievement                 = 6
	vtStatsClearAchievement               = 7
	vtStatsStoreStats                     = 9
	vtStatsGetAchievementDisplayAttribute = 11
	vtStatsGetNumAchievements             = 13
	vtStatsGetAchievementName             = 14
	vtStatsRequestUserStats               = 15

	cbUserStatsReceived     = 1101
	cbUserStatsStored       = 1102
	cbUserAchievementStored = 1103
	eResultOK               = 1
)

type achievementInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Desc     string `json:"desc"`
	Hidden   bool   `json:"hidden"`
	Achieved bool   `json:"achieved"`
	Icon     string `json:"icon"`
	IconGray string `json:"iconGray"`
}

func runSAMWorker(action, appid string) {
	out := samWork(action, appid)
	enc, _ := json.Marshal(out)
	os.Stdout.Write(enc)
	os.Stdout.Write([]byte("\n"))
}

func samFail(format string, a ...interface{}) map[string]interface{} {
	return map[string]interface{}{"ok": false, "error": fmt.Sprintf(format, a...)}
}

type samSession struct {
	createIface   uintptr
	getCallback   uintptr
	freeCallback  uintptr
	client        uintptr
	pipe          int32
	user          int32
	steamUser     uintptr
	steamUserStat uintptr
	steamApps     uintptr
	steamID       uint64
}

type statsStatus int

const (
	statsNotReady statsStatus = iota
	statsReady
	statsEmpty
)

func (s *samSession) isSubscribed(appid string) bool {
	if s.steamApps == 0 || !isAllDigits(appid) {
		return false
	}
	n, err := strconv.ParseUint(appid, 10, 32)
	if err != nil {
		return false
	}
	return byte(vcall(s.steamApps, vtAppsIsSubscribedApp, uintptr(uint32(n)))) != 0
}

func samWork(action, appid string) map[string]interface{} {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	steamPath := os.Getenv("ST_STEAM_PATH")
	if steamPath == "" {
		steamPath = samSteamPathFromRegistry()
	}
	if steamPath == "" {
		return samFail("Steam installation not found")
	}
	if os.Getenv("SteamAppId") == "" {

		_ = os.Setenv("SteamAppId", appid)
	}

	dllPath := filepath.Join(steamPath, "steamclient64.dll")
	if _, err := os.Stat(dllPath); err != nil {
		return samFail("steamclient64.dll not found in %s", steamPath)
	}

	samSetDllDirectory(steamPath)
	handle, err := windows.LoadLibraryEx(dllPath, 0, windows.LOAD_WITH_ALTERED_SEARCH_PATH)
	if err != nil {
		return samFail("failed to load steamclient64.dll: %v", err)
	}

	sess := &samSession{}
	if sess.createIface, err = windows.GetProcAddress(handle, "CreateInterface"); err != nil {
		return samFail("CreateInterface export missing: %v", err)
	}
	if sess.getCallback, err = windows.GetProcAddress(handle, "Steam_BGetCallback"); err != nil {
		return samFail("Steam_BGetCallback export missing: %v", err)
	}
	if sess.freeCallback, err = windows.GetProcAddress(handle, "Steam_FreeLastCallback"); err != nil {
		return samFail("Steam_FreeLastCallback export missing: %v", err)
	}

	cliVer := append([]byte(samClientVersion), 0)
	r, _, _ := syscall.SyscallN(sess.createIface, uintptr(unsafe.Pointer(&cliVer[0])), 0)
	runtime.KeepAlive(cliVer)
	sess.client = r
	if sess.client == 0 {
		return samFail("could not create %s interface", samClientVersion)
	}

	sess.pipe = int32(vcall(sess.client, vtClientCreateSteamPipe))
	if sess.pipe == 0 {
		return samFail("Steam is not running (or no user is logged in)")
	}
	defer vcall(sess.client, vtClientReleaseSteamPipe, uintptr(sess.pipe))

	sess.user = int32(vcall(sess.client, vtClientConnectToGlobalUser, uintptr(sess.pipe)))
	if sess.user == 0 {
		return samFail("could not connect to the logged-in Steam user")
	}

	statsVer := append([]byte(samStatsVersion), 0)
	sess.steamUserStat = vcall(sess.client, vtClientGetISteamUserStats,
		uintptr(sess.user), uintptr(sess.pipe), uintptr(unsafe.Pointer(&statsVer[0])))
	runtime.KeepAlive(statsVer)
	if sess.steamUserStat == 0 {
		return samFail("could not obtain ISteamUserStats")
	}

	appsVer := append([]byte(samAppsVersion), 0)
	sess.steamApps = vcall(sess.client, vtClientGetISteamApps,
		uintptr(sess.user), uintptr(sess.pipe), uintptr(unsafe.Pointer(&appsVer[0])))
	runtime.KeepAlive(appsVer)

	sess.steamID = samCurrentSteamID64()
	owned := sess.isSubscribed(appid)
	samDbg("pipe=%d user=%d steamID=%d owned=%v", sess.pipe, sess.user, sess.steamID, owned)
	if sess.steamID == 0 {
		return samFail("no Steam user is currently signed in")
	}

	vcall(sess.steamUserStat, vtStatsRequestUserStats, uintptr(sess.steamID))
	switch sess.waitStatsReady(18 * time.Second) {
	case statsReady:

	case statsEmpty:

		return map[string]interface{}{"ok": true, "achievements": []achievementInfo{}, "count": 0, "empty": true}
	default:
		if !owned {
			return samFail("You don't own this game on the signed-in Steam account")
		}
		return samFail("This game has no Steam achievements, or Steam couldn't load its stats. Launch it once in Steam, then retry")
	}

	switch action {
	case "list":
		return sess.listAchievements()
	case "apply":
		return sess.applyAchievements()
	default:
		return samFail("unknown worker action %q", action)
	}
}

func (s *samSession) waitStatsReady(timeout time.Duration) statsStatus {
	type callbackMsg struct {
		user     int32
		callback int32
		param    uintptr
		paramLen int32
		_        int32
	}
	start := time.Now()
	deadline := start.Add(timeout)
	reissued := false
	statsOK := false
	failCount := 0
	for time.Now().Before(deadline) {

		for {
			var msg callbackMsg
			var callID int32
			got, _, _ := syscall.SyscallN(s.getCallback,
				uintptr(s.pipe), uintptr(unsafe.Pointer(&msg)), uintptr(unsafe.Pointer(&callID)))
			if byte(got) == 0 {
				break
			}
			cb := msg.callback
			var result int32 = eResultOK
			if cb == cbUserStatsReceived && msg.param != 0 {
				result = *(*int32)(unsafe.Pointer(msg.param + 8))
			}
			syscall.SyscallN(s.freeCallback, uintptr(s.pipe))
			if cb == cbUserStatsReceived {
				samDbg("callback 1101 UserStatsReceived result=%d", result)
				if result == eResultOK {
					statsOK = true
				} else {
					failCount++
				}
			} else {
				samDbg("callback %d", cb)
			}
		}

		num := uint32(vcall(s.steamUserStat, vtStatsGetNumAchievements))
		if num > 0 {
			samDbg("GetNumAchievements=%d -> ready", num)
			return statsReady
		}

		if failCount >= 2 {
			samDbg("repeated UserStatsReceived failures -> not ready")
			return statsNotReady
		}

		if statsOK && time.Since(start) > 1500*time.Millisecond {
			samDbg("stats OK via callback, num=0 -> empty")
			return statsEmpty
		}

		if !reissued && time.Since(start) > timeout/2 {
			reissued = true
			samDbg("re-issuing RequestUserStats")
			vcall(s.steamUserStat, vtStatsRequestUserStats, uintptr(s.steamID))
		}
		time.Sleep(60 * time.Millisecond)
	}
	if uint32(vcall(s.steamUserStat, vtStatsGetNumAchievements)) > 0 {
		return statsReady
	}
	if statsOK {
		return statsEmpty
	}
	samDbg("deadline reached, not ready")
	return statsNotReady
}

var samDebug = os.Getenv("ST_SAM_DEBUG") != ""

func samDbg(format string, a ...interface{}) {
	if samDebug {
		fmt.Fprintf(os.Stderr, "[sam] "+format+"\n", a...)
	}
}

func (s *samSession) listAchievements() map[string]interface{} {
	num := uint32(vcall(s.steamUserStat, vtStatsGetNumAchievements))
	list := make([]achievementInfo, 0, num)
	for i := uint32(0); i < num; i++ {
		namePtr := vcall(s.steamUserStat, vtStatsGetAchievementName, uintptr(i))
		id := goStrFromPtr(namePtr)
		if id == "" {
			continue
		}
		info := achievementInfo{ID: id}
		info.Name = s.displayAttr(id, "name")
		info.Desc = s.displayAttr(id, "desc")
		info.Hidden = s.displayAttr(id, "hidden") == "1"
		if info.Name == "" {
			info.Name = id
		}
		info.Achieved = s.isAchieved(id)
		info.Icon = s.displayAttr(id, "icon")
		info.IconGray = s.displayAttr(id, "icon_gray")
		list = append(list, info)
	}
	return map[string]interface{}{"ok": true, "achievements": list, "count": len(list)}
}

func (s *samSession) applyAchievements() map[string]interface{} {
	raw, _ := io.ReadAll(os.Stdin)
	var changes struct {
		Unlock []string `json:"unlock"`
		Lock   []string `json:"lock"`
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &changes)
	}
	if len(changes.Unlock) == 0 && len(changes.Lock) == 0 {
		return map[string]interface{}{"ok": true, "changed": 0}
	}
	changed := 0
	for _, id := range changes.Unlock {
		if s.setAchievement(id, true) {
			changed++
		}
	}
	for _, id := range changes.Lock {
		if s.setAchievement(id, false) {
			changed++
		}
	}
	stored := byte(vcall(s.steamUserStat, vtStatsStoreStats)) != 0
	if !stored {
		return samFail("StoreStats failed — no changes were committed")
	}

	confirmed := s.waitStatsStored(6 * time.Second)
	samDbg("StoreStats confirmed=%v", confirmed)
	return map[string]interface{}{"ok": true, "changed": changed, "stored": true, "confirmed": confirmed}
}

func (s *samSession) waitStatsStored(timeout time.Duration) bool {
	type callbackMsg struct {
		user     int32
		callback int32
		param    uintptr
		paramLen int32
		_        int32
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for {
			var msg callbackMsg
			var callID int32
			got, _, _ := syscall.SyscallN(s.getCallback,
				uintptr(s.pipe), uintptr(unsafe.Pointer(&msg)), uintptr(unsafe.Pointer(&callID)))
			if byte(got) == 0 {
				break
			}
			cb := msg.callback
			var result int32 = eResultOK
			if cb == cbUserStatsStored && msg.param != 0 {

				result = *(*int32)(unsafe.Pointer(msg.param + 8))
			}
			syscall.SyscallN(s.freeCallback, uintptr(s.pipe))
			if cb == cbUserStatsStored {
				samDbg("callback 1102 UserStatsStored result=%d", result)
				return result == eResultOK
			}
			if cb == cbUserAchievementStored {
				samDbg("callback 1103 UserAchievementStored")
			}
		}
		time.Sleep(40 * time.Millisecond)
	}
	return false
}

func (s *samSession) displayAttr(id, key string) string {
	ib := append([]byte(id), 0)
	kb := append([]byte(key), 0)
	r := vcall(s.steamUserStat, vtStatsGetAchievementDisplayAttribute,
		uintptr(unsafe.Pointer(&ib[0])), uintptr(unsafe.Pointer(&kb[0])))
	runtime.KeepAlive(ib)
	runtime.KeepAlive(kb)
	return goStrFromPtr(r)
}

func (s *samSession) isAchieved(id string) bool {
	ib := append([]byte(id), 0)
	var achieved int32
	vcall(s.steamUserStat, vtStatsGetAchievement,
		uintptr(unsafe.Pointer(&ib[0])), uintptr(unsafe.Pointer(&achieved)))
	runtime.KeepAlive(ib)
	return achieved != 0
}

func (s *samSession) setAchievement(id string, unlock bool) bool {
	ib := append([]byte(id), 0)
	slot := vtStatsSetAchievement
	if !unlock {
		slot = vtStatsClearAchievement
	}
	r := vcall(s.steamUserStat, slot, uintptr(unsafe.Pointer(&ib[0])))
	runtime.KeepAlive(ib)
	return byte(r) != 0
}

func vcall(obj uintptr, index int, args ...uintptr) uintptr {
	vtable := *(*uintptr)(unsafe.Pointer(obj))
	fn := *(*uintptr)(unsafe.Pointer(vtable + uintptr(index)*unsafe.Sizeof(uintptr(0))))
	all := make([]uintptr, 0, len(args)+1)
	all = append(all, obj)
	all = append(all, args...)
	ret, _, _ := syscall.SyscallN(fn, all...)
	return ret
}

func goStrFromPtr(p uintptr) string {
	if p == 0 {
		return ""
	}
	var b []byte
	for i := uintptr(0); ; i++ {
		c := *(*byte)(unsafe.Pointer(p + i))
		if c == 0 {
			break
		}
		b = append(b, c)
	}
	return string(b)
}

func samSetDllDirectory(dir string) {
	k32 := windows.NewLazySystemDLL("kernel32.dll")
	proc := k32.NewProc("SetDllDirectoryW")
	if proc.Find() != nil {
		return
	}
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return
	}
	proc.Call(uintptr(unsafe.Pointer(p)))
}

func samCurrentSteamID64() uint64 {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Valve\Steam\ActiveProcess`, registry.QUERY_VALUE)
	if err != nil {
		return 0
	}
	defer k.Close()
	accountID, _, err := k.GetIntegerValue("ActiveUser")
	if err != nil || accountID == 0 {
		return 0
	}
	const individualBase = uint64(0x0110000100000000)
	return individualBase | uint64(uint32(accountID))
}

func samSteamPathFromRegistry() string {
	for _, regPath := range []string{`SOFTWARE\WOW6432Node\Valve\Steam`, `SOFTWARE\Valve\Steam`} {
		k, err := registry.OpenKey(registry.LOCAL_MACHINE, regPath, registry.QUERY_VALUE|windows.KEY_WOW64_64KEY)
		if err != nil {
			continue
		}
		val, _, err2 := k.GetStringValue("InstallPath")
		k.Close()
		if err2 == nil && strings.TrimSpace(val) != "" {
			return strings.TrimSpace(val)
		}
	}
	return ""
}

func (st *SteamTools) runAchWorker(action, appid string, stdin []byte, timeout time.Duration) (map[string]interface{}, error) {
	steam := st.getSteamStatus()
	if ok, _ := steam["detected"].(bool); !ok {
		return nil, fmt.Errorf("Steam installation not detected")
	}
	steamPath := toString(steam["path"])

	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(exe, "sam-worker", action, appid)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	cmd.Env = append(os.Environ(),
		"SteamAppId="+appid,
		"ST_STEAM_PATH="+steamPath,
	)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() { done <- cmd.Wait() }()

	select {
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("the Steam helper timed out")
	case <-done:
	}

	out := bytes.TrimSpace(stdout.Bytes())
	if len(out) == 0 {
		return nil, fmt.Errorf("the Steam helper returned no data")
	}

	if idx := bytes.LastIndexByte(out, '\n'); idx >= 0 {
		out = bytes.TrimSpace(out[idx+1:])
	}
	var result map[string]interface{}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("could not parse the Steam helper response")
	}
	return result, nil
}

func achMetaCacheFile() string { return filepath.Join(customLocationDir(), "ach_meta_cache.json") }

var achNameMu sync.Mutex

func (st *SteamTools) achievementAppName(appid string) map[string]interface{} {
	appid = strings.TrimSpace(appid)
	if !isAllDigits(appid) {
		return map[string]interface{}{"ok": false, "error": "invalid appid"}
	}

	achNameMu.Lock()
	meta, _ := readJSONMap(achMetaCacheFile())
	if entry, ok := meta[appid].(map[string]interface{}); ok {
		achNameMu.Unlock()
		return map[string]interface{}{"ok": true, "appid": appid, "name": toString(entry["name"]), "isGame": toBool(entry["game"])}
	}
	achNameMu.Unlock()

	name, typ, got := st.fetchAppNameType(appid)
	isGame := typ == "game"
	if !got {

		name = st.fetchGameName(appid)
		isGame = false
	}

	achNameMu.Lock()
	meta, _ = readJSONMap(achMetaCacheFile())
	if meta == nil {
		meta = map[string]interface{}{}
	}
	meta[appid] = map[string]interface{}{"name": name, "game": isGame}
	writeJSONMap(achMetaCacheFile(), meta)
	if name != "" {
		nc, _ := readJSONMap(nameCacheFile())
		if nc == nil {
			nc = map[string]interface{}{}
		}
		nc[appid] = name
		writeJSONMap(nameCacheFile(), nc)
	}
	achNameMu.Unlock()
	return map[string]interface{}{"ok": true, "appid": appid, "name": name, "isGame": isGame}
}

func (st *SteamTools) fetchAppNameType(appid string) (name, typ string, got bool) {
	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	url := fmt.Sprintf("https://store.steampowered.com/api/appdetails?appids=%s&cc=us&l=english&filters=basic", appid)
	body, status, err := httpGetBytes(url, 8*time.Second, map[string]string{"User-Agent": ua, "Referer": "https://store.steampowered.com/"})
	if err != nil || status != 200 {
		return "", "", false
	}
	var data map[string]interface{}
	if json.Unmarshal(body, &data) != nil {
		return "", "", false
	}
	entry, ok := data[appid].(map[string]interface{})
	if !ok {
		return "", "", false
	}
	if success, _ := entry["success"].(bool); !success {
		return "", "", false
	}
	d, ok := entry["data"].(map[string]interface{})
	if !ok {
		return "", "", false
	}
	return toString(d["name"]), strings.ToLower(toString(d["type"])), true
}

func (st *SteamTools) achievementsList(appid string) map[string]interface{} {
	res, err := st.runAchWorker("list", appid, nil, 12*time.Second)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return res
}

func (st *SteamTools) achievementsApply(appid string, unlock, lock []string) map[string]interface{} {
	payload, _ := json.Marshal(map[string]interface{}{"unlock": unlock, "lock": lock})
	res, err := st.runAchWorker("apply", appid, payload, 15*time.Second)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return res
}

func (st *SteamTools) listOwnedGames() map[string]interface{} {
	steam := st.getSteamStatus()
	if ok, _ := steam["detected"].(bool); !ok {
		return map[string]interface{}{"ok": false, "error": "Steam installation not detected"}
	}
	steamPath := toString(steam["path"])

	names := map[string]string{}
	installed := map[string]bool{}
	order := []string{}
	add := func(appid, name string) {
		if appid == "" {
			return
		}
		if _, ok := names[appid]; !ok {
			order = append(order, appid)
		}
		if name != "" {
			names[appid] = name
		} else if _, ok := names[appid]; !ok {
			names[appid] = ""
		}
	}

	for _, lib := range steamLibraryFolders(steamPath) {
		appsDir := filepath.Join(lib, "steamapps")
		entries, err := os.ReadDir(appsDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			n := e.Name()
			if !strings.HasPrefix(n, "appmanifest_") || !strings.HasSuffix(n, ".acf") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(appsDir, n))
			if err != nil {
				continue
			}
			appid := acfValue(data, "appid")
			title := acfValue(data, "name")
			if appid == "" || isNonGameApp(appid, title) {
				continue
			}
			installed[appid] = true
			add(appid, title)
		}
	}

	nameCache, _ := readJSONMap(nameCacheFile())
	for _, appid := range st.ownedAppIDsFromConfig(steamPath) {
		if isNonGameApp(appid, "") {
			continue
		}
		cached := ""
		if nameCache != nil {
			if s, ok := nameCache[appid].(string); ok {
				cached = s
			}
		}
		add(appid, cached)
	}

	games := make([]map[string]interface{}, 0, len(order))
	for _, appid := range order {
		games = append(games, map[string]interface{}{
			"appid":     appid,
			"name":      names[appid],
			"installed": installed[appid],
		})
	}

	sort.SliceStable(games, func(i, j int) bool {
		ii, _ := games[i]["installed"].(bool)
		ji, _ := games[j]["installed"].(bool)
		if ii != ji {
			return ii
		}
		ni := strings.ToLower(toString(games[i]["name"]))
		nj := strings.ToLower(toString(games[j]["name"]))
		if (ni == "") != (nj == "") {
			return ni != ""
		}
		return ni < nj
	})
	return map[string]interface{}{"ok": true, "games": games, "total": len(games)}
}

func (st *SteamTools) ownedAppIDsFromConfig(steamPath string) []string {
	steamID := samCurrentSteamID64()
	if steamID == 0 {
		return nil
	}
	accountID := uint32(steamID)
	path := filepath.Join(steamPath, "userdata", fmt.Sprint(accountID), "config", "localconfig.vdf")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return parseLocalConfigApps(data)
}

func parseLocalConfigApps(data []byte) []string {
	text := string(data)

	idx := strings.Index(text, "\"apps\"")
	if idx < 0 {
		return nil
	}
	rest := text[idx+len("\"apps\""):]
	open := strings.IndexByte(rest, '{')
	if open < 0 {
		return nil
	}
	rest = rest[open+1:]

	out := []string{}
	seen := map[string]bool{}
	depth := 0
	i := 0
	for i < len(rest) {
		c := rest[i]
		switch c {
		case '{':
			depth++
			i++
		case '}':
			if depth == 0 {
				return out
			}
			depth--
			i++
		case '"':

			j := strings.IndexByte(rest[i+1:], '"')
			if j < 0 {
				return out
			}
			token := rest[i+1 : i+1+j]
			i = i + 1 + j + 1

			if depth == 0 && isAllDigits(token) && !seen[token] {
				seen[token] = true
				out = append(out, token)
			}
		default:
			i++
		}
	}
	return out
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isNonGameApp(appid, name string) bool {
	switch appid {
	case "228980",
		"1070560",
		"1391110",
		"1628350",
		"1493710",
		"2805730",
		"4183110":
		return true
	}
	low := strings.ToLower(name)
	for _, kw := range []string{"proton", "steam linux runtime", "steamworks common redistributables", "steam runtime"} {
		if strings.Contains(low, kw) {
			return true
		}
	}
	return false
}

func steamLibraryFolders(steamPath string) []string {
	libs := []string{steamPath}
	vdf := filepath.Join(steamPath, "steamapps", "libraryfolders.vdf")
	data, err := os.ReadFile(vdf)
	if err != nil {
		return libs
	}

	text := string(data)
	for {
		idx := strings.Index(text, "\"path\"")
		if idx < 0 {
			break
		}
		text = text[idx+len("\"path\""):]
		q1 := strings.IndexByte(text, '"')
		if q1 < 0 {
			break
		}
		rest := text[q1+1:]
		q2 := strings.IndexByte(rest, '"')
		if q2 < 0 {
			break
		}
		p := strings.ReplaceAll(rest[:q2], `\\`, `\`)
		if p != "" && !strings.EqualFold(p, steamPath) {
			libs = append(libs, p)
		}
		text = rest[q2+1:]
	}
	return libs
}

func acfValue(data []byte, key string) string {
	needle := "\"" + key + "\""
	text := string(data)
	idx := strings.Index(text, needle)
	if idx < 0 {
		return ""
	}
	rest := text[idx+len(needle):]
	q1 := strings.IndexByte(rest, '"')
	if q1 < 0 {
		return ""
	}
	rest = rest[q1+1:]
	q2 := strings.IndexByte(rest, '"')
	if q2 < 0 {
		return ""
	}
	return rest[:q2]
}
