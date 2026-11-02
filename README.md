# PortTide

A local proxy subscription manager for Windows, with a tray icon, background operation and an embedded browser interface. **Early alpha.**


[Инструкция на русском](README.ru.md)

PortTide imports unencrypted Xray JSON subscriptions, preserves profile names/emoji and available subscription notices, selects a profile, and exposes a local HTTP/SOCKS5 proxy. It provides **proxy mode only**. Route traffic into it using an external client such as ZeroOmega, Clash Verge, or an application's proxy settings. **Traffic filtering is not configured**: the external client chooses which sites/applications use the proxy. PortTide does not change system proxy, TUN, DNS or firewall settings.

## Start

1. Run the Windows x64 installer. Installation is per-user, without elevation; choose login startup and an optional desktop shortcut.
2. Open PortTide from Start, the desktop shortcut or its tray icon. The local interface uses `http://127.0.0.1:17790`.
3. Add an HTTPS subscription URL or import an Xray JSON file. The User-Agent is editable; the default `Happ/2.0.0` requests a commonly used Xray JSON subscription format.
4. Inspect a profile, run diagnostics, and connect it.
5. Configure your client to use `127.0.0.1:17890` as HTTP or SOCKS5. Open proxy settings for the generated username/password.

A portable ZIP is also supported: extract it completely, including `core`. Use `PortTide.exe --open` to explicitly open the panel. If you move a portable folder, enable startup again to register its new location.

The default is a mixed port with password authentication. A separate HTTP port is optional and initially unset. Mixed and SOCKS5 share the same saved port value; the separate HTTP port is retained when hidden, and changing mode never resets either value. All listeners bind to loopback. The native Xray SOCKS listener also accepts HTTP when separate mode is selected; the additional HTTP listener provides a distinct client endpoint.

## Everyday use

Application settings separate **start at Windows logon**, **connect on app launch** (default on), and **launch in background** (default on). Installer login startup is offered enabled; a portable instance requires opting in through settings. Explicit shortcuts and the tray Open command always open the panel. First interactive launch without subscriptions opens setup. Launching again hands off to the existing instance.

Closing the browser leaves the proxy running. Stop affects only the current session; the next normal launch reconnects when enabled. Quit from the tray or Application settings stops both PortTide and its owned core. Startup uses the last supported profile, then the first supported profile in the same source, then source order. Provider names are never special-cased. Connection errors do not select another country. Startup uses the cached config before background refresh.

## Application updates

Application settings can check GitHub Releases, open release notes, and download/start the installer. Checks run after startup and daily. Installation is always initiated by the user. Alpha versions include prereleases; stable versions only offer stable releases. Downloads must match the official repository asset URL, size and SHA-256 digest. A custom `--data-dir` instance must be updated manually.

The program lives at `%LOCALAPPDATA%/Programs/PortTide`; data remain separate. Upgrades preserve subscription data, ports, credentials, selection, shortcuts and startup. A settings backup is saved before installation. Download completes before the proxy is stopped. Installation briefly interrupts connections and restores the previous running/stopped state without opening a browser. The panel reconnects automatically. Bundled Xray is updated with the app; a configured external core is not overwritten.

The same installer can upgrade an existing installation manually. When migrating from the old `0.1.0-alpha.2` ZIP, quit that version first. Uninstall keeps user data by default and offers an explicit removal choice.

## Checks

- HTTPS delay: a warmup request followed by seven samples over reusable connections, with median, range and variation of successful samples. This is application-layer latency, not ICMP ping.
- Throughput: four parallel adaptive transfers per direction, a separate warmup (up to 3 seconds of requests), then up to 20 seconds of measurement. Each request is capped at 4 MiB; all warmup/measurement payload is capped at 1 GiB per direction. Warmup is excluded, uploads require successful server acknowledgement, and early limits/failures are reported. This is measured application goodput to Cloudflare, not a guarantee of maximum line capacity.
- Geography: Cloudflare and ipify observe their exit IPs. ipwho.is and ipapi.co are queried explicitly for each observed IP. Conflicting results remain separate. Service failures/rate limits are shown rather than guessed.

Diagnostics open in a side drawer without moving the profile list. Country flags are bundled SVGs and subscription notices/usage appear below the Profiles heading.

Each diagnostic uses its own temporary Xray process/port. It never switches the production profile. Only one diagnostic runs at a time. Checks share the computer's physical bandwidth and use normal OS routing to reach proxy servers. Results currently last for the application session.

## Data and configuration

User data is stored in `%LOCALAPPDATA%/PortTide` on Windows (`PortTide` under the user config directory on Linux). `settings.json` contains subscription URLs, local proxy credentials, selection and preferences; `cache.json` contains imported configurations. Treat both as private. Do not put them into this repository or share them with a friend. Share a clean installer or ZIP instead.

To preconfigure an installation, adapt [examples/settings.json](examples/settings.json) in the data directory while PortTide is stopped. Missing IDs and proxy passwords are generated on first start. Use schema version 2; legacy version 1 is migrated with a backup. Leave `corePath` omitted/empty to find the `core` directory beside the executable, so moving the bundle remains possible.

CLI options: `--data-dir PATH`, `--listen 127.0.0.1:PORT`, `--core PATH`, `--open`, `--background`, `--no-browser`, `--version`. Overrides make isolated testing possible.

## Subscription behavior

Supports a single Xray configuration object or an array of configurations, up to 4 MiB / 512 entries. The catalog reports unsupported protocol/configuration shapes. Xray validates a configuration again before activation. Encrypted subscriptions, URI/base64 subscriptions, Clash YAML and sing-box JSON are not supported yet.

PortTide replaces provider listener/API/log settings and applies a proxy-only user-traffic route. Supported proxy transports, relay settings and balancers are preserved; provider per-site DIRECT/REJECT rules are not imported. Direct bootstrap DNS rules are restricted to internal DNS traffic. Direct balancer fallback and disabled TLS verification are rejected. Server-side routing performed by the VPN provider is outside the client's control.

A failed download or invalid update keeps the cached profiles. When an active profile disappears or becomes unsupported, refresh is refused until it is stopped. Changes to an active profile restart only PortTide's Xray process and may interrupt its existing connections. If startup fails, PortTide attempts to restore the previous runtime. A running process means its listener is ready, not that the remote server was verified.

## External routing example

Add a local SOCKS5 proxy with the address, port and credentials shown in PortTide, then select it in the relevant Clash group. Ensure the actual PortTide `core/xray.exe` path is excluded from interception when using a TUN client; otherwise a routing loop is possible. PortTide does not edit those rules. Nothing requires replacing your existing profiles or proxy groups.

## Development

Requires Go matching `go.mod`; Python 3.12+ is only used for packaging/core download. The UI is plain embedded HTML/CSS/JavaScript. No Node runtime is required by the shipped application.

```sh
python3 scripts/fetch-core.py --platform linux-amd64
PORTTIDE_TEST_CORE="$PWD/core/xray" go test -race ./...
go vet ./...
go run ./cmd/porttide --data-dir .local --no-browser
```

On Windows, fetch `--platform windows-amd64`, set `PORTTIDE_TEST_CORE` to the absolute path of `core/xray.exe`, and run `go test ./...`. Core-less unit tests run without that variable; integration tests explicitly report a skip.

```sh
python3 scripts/package.py
```

On Windows, build the installer from that clean bundle:

```powershell
./scripts/fetch-inno.ps1
python scripts/build-installer.py --iscc .tools/inno/ISCC.exe
```

The pinned Inno Setup compiler is extracted in portable mode. To validate installation without touching production data/startup, pass `--test-data-dir` to build an installer with a separate test AppId, shortcuts and UI port 17794. Use a temporary installation directory and uninstall it after verification. Internal `--command` and `--resume` flags coordinate the installer; normal shortcuts use `--open`.

Packaging creates a Windows executable, pinned Xray assets, licenses and SHA-256 checksum under `dist/`. The original user data is never included. The build workflow runs native Windows tests and Linux race checks; it does not need a real subscription. Core version and archive checksums are recorded in [core-manifest.json](core-manifest.json).

See [implementation notes](docs/implementation.md) and [third-party notices](THIRD_PARTY_NOTICES.md). PortTide is MIT licensed.
