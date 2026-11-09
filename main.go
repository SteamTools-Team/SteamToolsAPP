package main

import (
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
)

var (
	serverPort int
	serverHWID string
)

func main() {

	if len(os.Args) >= 4 && os.Args[1] == "sam-worker" {
		runSAMWorker(os.Args[2], os.Args[3])
		return
	}

	serverHWID = getHWID()

	if checkLocalBan(serverHWID) {
		os.Exit(1)
	}

	terminatePriorInstance()

	serverPort = findFreePort()
	writeLock(serverPort)
	defer deleteLock()

	loadSession()

	srv := &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", serverPort), Handler: apiHandler{}}
	go func() {
		_ = srv.ListenAndServe()
	}()

	if !waitForServer(serverPort, 15*time.Second) {
		os.Exit(1)
	}

	if checkHWIDBanned(serverHWID) {
		writeLocalBan(serverHWID)
		os.Exit(1)
	}

	runWebView()
}

func checkHWIDBanned(hwid string) bool {
	if hwid == "" {
		return false
	}
	status, result := apiPost("/api/desktop/security-alert", map[string]interface{}{"hwid": hwid, "checkOnly": true}, "", 8*time.Second)
	return status == 200 && toBool(result["banned"])
}

var (
	user32Dll            = windows.NewLazySystemDLL("user32.dll")
	showWindowProc       = user32Dll.NewProc("ShowWindow")
	getWindowLongProc    = lazyProcOr("GetWindowLongPtrW", "GetWindowLongW")
	setWindowLongProc    = lazyProcOr("SetWindowLongPtrW", "SetWindowLongW")
	setWindowPosProc     = user32Dll.NewProc("SetWindowPos")
	getWindowRectProc    = user32Dll.NewProc("GetWindowRect")
	getSystemMetricsProc = user32Dll.NewProc("GetSystemMetrics")
	callWindowProcProc   = user32Dll.NewProc("CallWindowProcW")
	loadImageWProc       = user32Dll.NewProc("LoadImageW")
	sendMessageWProc     = user32Dll.NewProc("SendMessageW")
	getModuleHandleProc  = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW")
	dwmSetWindowAttr     = windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")
	originalWndProc      uintptr
	resizeWndProc        uintptr
)

func lazyProcOr(primary, fallback string) *windows.LazyProc {
	p := user32Dll.NewProc(primary)
	if err := p.Find(); err == nil {
		return p
	}
	return user32Dll.NewProc(fallback)
}

type nativeRect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

func runWebView() {

	mica := supportsMica()
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		WindowOptions: webview2.WindowOptions{Transparent: mica},
	})
	if w == nil {
		openBrowser(fmt.Sprintf("http://127.0.0.1:%d/", serverPort))
		select {}
	}
	defer w.Destroy()

	w.SetTitle("SteamTools")
	hwnd := uintptr(w.Window())

	_, _, _ = showWindowProc.Call(hwnd, 0)

	makeFrameless(hwnd)

	if mica && applyMica(hwnd) {
		w.Init(`(function () {
  function on() { if (document.documentElement) document.documentElement.classList.add('st-translucent'); }
  on();
  document.addEventListener('readystatechange', on);
})();`)
	}

	var (
		showOnceMu   sync.Mutex
		showWindowed bool
	)
	showOnce := func() {
		showOnceMu.Lock()
		defer showOnceMu.Unlock()
		if showWindowed {
			return
		}
		showWindowed = true
		w.Dispatch(func() {

			screenW, _, _ := getSystemMetricsProc.Call(0)
			screenH, _, _ := getSystemMetricsProc.Call(1)
			const winW, winH = 1440, 860
			x := (int(int32(screenW)) - winW) / 2
			if x < 0 {
				x = 0
			}
			y := (int(int32(screenH)) - winH) / 2
			if y < 0 {
				y = 0
			}
			_, _, _ = setWindowPosProc.Call(hwnd, 0, uintptr(x), uintptr(y), winW, winH, 0x0004|0x0040|0x0020)

			var r nativeRect
			_, _, _ = getWindowRectProc.Call(hwnd, uintptr(unsafe.Pointer(&r)))
			moveLP := uintptr(uint32(uint16(r.Left&0xFFFF)) | (uint32(uint16(r.Top&0xFFFF)) << 16))
			sizeLP := uintptr(uint32(uint16((r.Right-r.Left)&0xFFFF)) | (uint32(uint16((r.Bottom-r.Top)&0xFFFF)) << 16))
			_, _, _ = sendMessageWProc.Call(hwnd, 0x0003, 0, moveLP)
			_, _, _ = sendMessageWProc.Call(hwnd, 0x0005, 0, sizeLP)
		})
	}
	_ = w.Bind("st_ready", func() { showOnce() })
	time.AfterFunc(3*time.Second, showOnce)

	_ = w.Bind("st_minimize", func() {
		w.Dispatch(func() { _, _, _ = showWindowProc.Call(hwnd, 6) })
	})
	_ = w.Bind("st_close", func() {
		w.Dispatch(func() { w.Terminate() })
	})
	_ = w.Bind("st_move_window", func(x, y int) {
		w.Dispatch(func() {
			_, _, _ = setWindowPosProc.Call(hwnd, 0, uintptr(x), uintptr(y), 0, 0, 0x0004|0x0001)
		})
	})
	_ = w.Bind("st_open_browser", func(url string) {
		openBrowser(url)
	})
	_ = w.Bind("st_remove_game", func(appid string) map[string]interface{} {
		return api.removeGame(appid)
	})
	_ = w.Bind("st_pick_folder", func() string {
		return pickFolder()
	})
	_ = w.Bind("st_pick_file", func() string {
		return pickFile()
	})

	w.Init(shimJS())

	w.Navigate(fmt.Sprintf("http://127.0.0.1:%d/", serverPort))
	w.Run()
}

func shimJS() string {
	return `(function () {
  function makePywebview() {
    window.pywebview = { api: {
      minimize: function () { return window.st_minimize ? window.st_minimize() : null; },
      close: function () { return window.st_close ? window.st_close() : null; },
      open_browser: function (url) { return window.st_open_browser ? window.st_open_browser(url) : null; },
      remove_game: function (appid) { return window.st_remove_game ? window.st_remove_game(appid) : null; },
      pick_folder: function () { return window.st_pick_folder ? window.st_pick_folder() : null; },
      pick_file: function () { return window.st_pick_file ? window.st_pick_file() : null; }
    } };
    var s = document.createElement('style');
    s.textContent = '* { -webkit-app-region: drag; } .content-area, .sidebar-footer, #modal-overlay, button, a, input, select, textarea, [contenteditable] { -webkit-app-region: no-drag; }';
    (document.head || document.documentElement).appendChild(s);
    try { window.dispatchEvent(new Event('pywebviewready')); } catch (e) {}
  }

  function bindWindowControls() {
    var mi = document.getElementById('wc-minimize');
    var cl = document.getElementById('wc-close');
    if (mi && !mi.dataset.stBound) { mi.dataset.stBound = '1'; mi.addEventListener('click', function () { window.pywebview.api.minimize(); }); }
    if (cl && !cl.dataset.stBound) { cl.dataset.stBound = '1'; cl.addEventListener('click', function () { window.pywebview.api.close(); }); }
  }

  var NO_DRAG = 'button, a, input, select, textarea, [contenteditable], .content-area, .sidebar-footer, #modal-overlay';
  function isNoDrag(el) {
    var n = el;
    while (n && n !== document.body) {
      if (n.matches && n.matches(NO_DRAG)) return true;
      n = n.parentElement;
    }
    return false;
  }

  var _drag = null;
  function dragStart(e) {
    if (e.button !== 0 || isNoDrag(e.target)) return;
    _drag = { sx: e.screenX, sy: e.screenY, wx: window.screenX, wy: window.screenY };
    e.preventDefault();
  }
  function dragMove(e) {
    if (!_drag) return;
    if (window.st_move_window) window.st_move_window(_drag.wx + (e.screenX - _drag.sx), _drag.wy + (e.screenY - _drag.sy));
  }
  function dragEnd() { _drag = null; }

  function suppressBrowserChrome() {
    var css = document.createElement('style');
    css.textContent = [
      'a, button, img, [draggable="true"] { -webkit-user-drag: none !important; }',
      'input, textarea, [contenteditable] { -webkit-user-select: text !important; }',
      'body { -webkit-user-select: none; }'
    ].join('\n');
    (document.head || document.documentElement).appendChild(css);

    ['dragstart', 'drag', 'dragover', 'drop', 'dragend'].forEach(function (ev) {
      window.addEventListener(ev, function (e) { if (e.preventDefault) e.preventDefault(); }, true);
    });

    function neutralizeInputs(el) {
      if (!el) return;
      el.setAttribute('autocomplete', 'off');
      el.setAttribute('autocorrect', 'off');
      el.setAttribute('autocapitalize', 'off');
      el.setAttribute('spellcheck', 'false');
    }
    document.querySelectorAll('input, textarea').forEach(function (el) { neutralizeInputs(el); });
    new MutationObserver(function (ms) {
      ms.forEach(function (m) {
        m.addedNodes.forEach(function (n) {
          if (n.nodeType !== 1) return;
          if (n.matches && n.matches('input, textarea')) neutralizeInputs(n);
          var list = n.querySelectorAll && n.querySelectorAll('input, textarea');
          if (list) list.forEach(function (el) { neutralizeInputs(el); });
        });
      });
    }).observe(document.documentElement, { childList: true, subtree: true });
  }

  function boot() {
    suppressBrowserChrome();
    makePywebview();
    bindWindowControls();
    document.addEventListener('mousedown', dragStart, true);
    document.addEventListener('mousemove', dragMove, true);
    document.addEventListener('mouseup', dragEnd, true);
    if (window.st_ready) { window.st_ready(); }
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', boot);
  } else {
    boot();
  }
})();`
}

func makeFrameless(hwnd uintptr) {
	var (
		gwlStyle    int32 = -16
		gwlpWndProc int32 = -4
	)
	const (
		wsCaption       = 0x00C00000
		wsThickFrame    = 0x00040000
		wsSysMenu       = 0x00080000
		wsMinimizeBox   = 0x00020000
		wsMaximizeBox   = 0x00010000
		wsPopup         = 0x80000000
		swpFramechanged = 0x0020
		swpNoMove       = 0x0002
		swpNoZOrder     = 0x0004
	)
	style, _, _ := getWindowLongProc.Call(hwnd, uintptr(gwlStyle))
	style &^= wsCaption | wsThickFrame | wsSysMenu | wsMinimizeBox | wsMaximizeBox
	style |= wsPopup
	_, _, _ = setWindowLongProc.Call(hwnd, uintptr(gwlStyle), style)

	original, _, _ := getWindowLongProc.Call(hwnd, uintptr(gwlpWndProc))
	originalWndProc = original
	resizeWndProc = windows.NewCallback(resizeProc)
	_, _, _ = setWindowLongProc.Call(hwnd, uintptr(gwlpWndProc), resizeWndProc)

	_, _, _ = setWindowPosProc.Call(hwnd, 0, 0, 0, 1440, 860, uintptr(swpFramechanged|swpNoMove|swpNoZOrder))

	roundWindowCorners(hwnd)
	setWindowIcon(hwnd)
}

func setWindowIcon(hwnd uintptr) {
	hinstance, _, _ := getModuleHandleProc.Call(0)
	if hinstance == 0 {
		return
	}
	const (
		imageIcon     = 1
		wmSetIcon     = 0x0080
		iconSmall     = 0
		iconBig       = 1
		lrDefaultSize = 0x0040
	)
	big, _, _ := loadImageWProc.Call(hinstance, 1, imageIcon, 0, 0, lrDefaultSize)
	if big != 0 {
		_, _, _ = sendMessageWProc.Call(hwnd, wmSetIcon, iconBig, big)
	}
	small, _, _ := loadImageWProc.Call(hinstance, 1, imageIcon, 16, 16, 0)
	if small != 0 {
		_, _, _ = sendMessageWProc.Call(hwnd, wmSetIcon, iconSmall, small)
	}
}

func roundWindowCorners(hwnd uintptr) {
	const (
		dwmwaWindowCornerPreference = 33
		dwmcpRound                  = 2
	)
	pref := uint32(dwmcpRound)
	_, _, _ = dwmSetWindowAttr.Call(hwnd, uintptr(dwmwaWindowCornerPreference), uintptr(unsafe.Pointer(&pref)), unsafe.Sizeof(pref))
}

var dwmExtendFrameProc = windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmExtendFrameIntoClientArea")

func supportsMica() bool {
	v := windows.RtlGetVersion()
	return v.MajorVersion >= 10 && v.BuildNumber >= 22621
}

func applyMica(hwnd uintptr) bool {
	const (
		dwmwaUseImmersiveDarkMode = 20
		dwmwaSystemBackdropType   = 38
		dwmsbtTransientWindow     = 3
	)
	dark := uint32(1)
	_, _, _ = dwmSetWindowAttr.Call(hwnd, uintptr(dwmwaUseImmersiveDarkMode), uintptr(unsafe.Pointer(&dark)), unsafe.Sizeof(dark))

	margins := [4]int32{-1, -1, -1, -1}
	if hr, _, _ := dwmExtendFrameProc.Call(hwnd, uintptr(unsafe.Pointer(&margins))); hr != 0 {
		return false
	}
	backdrop := uint32(dwmsbtTransientWindow)
	hr, _, _ := dwmSetWindowAttr.Call(hwnd, uintptr(dwmwaSystemBackdropType), uintptr(unsafe.Pointer(&backdrop)), unsafe.Sizeof(backdrop))
	return hr == 0
}

func resizeProc(hwnd, msg, wp, lp uintptr) uintptr {
	if msg == 0x0084 {
		ptX := int16(uint16(lp & 0xFFFF))
		ptY := int16(uint16((lp >> 16) & 0xFFFF))
		var rc nativeRect
		_, _, _ = getWindowRectProc.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
		x := int(ptX) - int(rc.Left)
		y := int(ptY) - int(rc.Top)
		w := int(rc.Right - rc.Left)
		h := int(rc.Bottom - rc.Top)
		const border = 6
		onLeft := x < border
		onRight := x >= w-border
		onTop := y < border
		onBottom := y >= h-border
		switch {
		case onTop && onLeft:
			return 13
		case onTop && onRight:
			return 14
		case onBottom && onLeft:
			return 16
		case onBottom && onRight:
			return 17
		case onLeft:
			return 10
		case onRight:
			return 11
		case onTop:
			return 12
		case onBottom:
			return 15
		}
	}
	r, _, _ := callWindowProcProc.Call(originalWndProc, hwnd, msg, wp, lp)
	return r
}
