<p align="center">
  <img src="web/icon.png" width="96" alt="SteamTools logo">
</p>

<h1 align="center">SteamTools</h1>

<p align="center">
  A fast, lightweight desktop app for managing Steam games, fixes and achievements.<br>
  Written in Go with a WebView2 UI.
</p>

---

## About

This is the **public, open-source** release of SteamTools, published for transparency so the
community can read and audit the code.

> **Note on this build**
>   endpoints before building (see [Configuration](#configuration)).
>   encrypted local storage). The machine-ID based **account-protection** (ban enforcement) is kept.
> - This repository is provided **for transparency**; it is not guaranteed to run end-to-end
>   without a compatible backend.

## Features

- Install / update Steam games from a configured manifest provider
- Steam diagnostics and repair
- Game fixes
- **Achievement manager** — unlock / lock achievements for games you own (via the Steam client)
- Multiple themes, including a translucent "Glass" theme

## Building

Requirements:
- [Go](https://go.dev/dl/) 1.26+
- Windows (amd64)
- WebView2 runtime (ships with modern Windows)

```bat
Build.bat
```

or directly:

```bat
set GOOS=windows
set GOARCH=amd64
go build -ldflags "-s -w -H=windowsgui" -o SteamTools.exe .
```

The window icon, version info and manifest are embedded from `rsrc.syso`
(regenerate with [goversioninfo](https://github.com/josephspurrier/goversioninfo) if you
change `web/icon.ico`, `versioninfo.json` or `SteamTools.exe.manifest`).

## Configuration

Replace the placeholder endpoints with your own before building:

| Placeholder | File | Constant |
|---|---|---|
| `https://hubcap.example.com` | `winutil.go` | `HUBCAP_BASE` |
| `https://depotbox.example.com` | `winutil.go` | `DEPOTBOX_BASE` |
| `https://fixes.example.com` | `winutil.go` | `FIXES_BASE` |
| `https://api.example.com` | `winutil.go` | `WEBSITE_BASE` |
| `https://raw.githubusercontent.com/YOUR-ORG/YOUR-CONFIG-REPO/...` | `winutil.go` | `CONFIG_JSON_URL` |
| `https://raw.githubusercontent.com/YOUR-ORG/YOUR-RELEASE-REPO/...` | `backend.go` | auto-update URL |

(There are a few more `example.com` / `YOUR-ORG` references in the code — search for them.)

## License

Licensed under the **GNU General Public License v3.0**. See [`LICENSE`](LICENSE).

## Disclaimer

Provided for educational and transparency purposes. Use it in accordance with Steam's terms of
service and the laws of your jurisdiction. The authors take no responsibility for misuse.
