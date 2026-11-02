# PortTide

A local proxy subscription manager for Windows, with an embedded browser interface. **Early alpha.**

[Инструкция на русском](README.ru.md)

PortTide imports unencrypted Xray JSON subscriptions, preserves profile names/emoji and available subscription notices, selects a profile, and exposes a local HTTP/SOCKS5 proxy. It does **not** change system proxy, TUN, DNS, firewall, or application routing.

## Start

1. Download a portable Windows x64 bundle from a release or the Windows CI artifact and extract it completely.
2. Run `PortTide.exe`. The local interface opens at `http://127.0.0.1:17790`.
3. Add an HTTPS subscription URL or import an Xray JSON file. The User-Agent is editable; the default `Happ/2.0.0` requests a commonly used Xray JSON subscription format.
4. Inspect a profile, run diagnostics, and connect it.
5. Configure your client to use `127.0.0.1:17890` as HTTP or SOCKS5. Open proxy settings for the generated username/password.

The default is a mixed port with password authentication. A separate HTTP port is optional. All listeners bind to loopback. The native Xray SOCKS listener also accepts HTTP when separate mode is selected; the additional HTTP listener provides a distinct client endpoint.

Closing the browser tab does not stop the application. Use **Settings → Quit PortTide** to exit. Stop disables automatic profile restoration; otherwise the selected profile is restored when PortTide is launched again. The application does not install Windows startup entries automatically.

## Checks

- HTTPS delay: three fresh requests to Google gstatic, with median of successful samples. This is application-layer latency, not ICMP ping.
- Throughput: manual Cloudflare download (at most 20 MB) and upload (4 MiB), up to 15 seconds per direction. It measures a bounded transfer including connection overhead, not maximum line capacity.
- Geography: Cloudflare and ipify observe their exit IPs. ipwho.is and ipapi.co are queried explicitly for each observed IP. Conflicting results remain separate. Service failures/rate limits are shown rather than guessed.

Each diagnostic uses its own temporary Xray process/port. It never switches the production profile. Only one diagnostic runs at a time. Checks share the computer's physical bandwidth and use normal OS routing to reach proxy servers. Results currently last for the application session.

## Data and configuration

User data is stored in `%LOCALAPPDATA%/PortTide` on Windows (`PortTide` under the user config directory on Linux). `settings.json` contains subscription URLs, local proxy credentials, selection and preferences; `cache.json` contains imported configurations. Treat both as private. Do not put them into this repository or share them with a friend. Share a clean portable bundle instead.

To preconfigure an installation, adapt [examples/settings.json](examples/settings.json) in the data directory while PortTide is stopped. Missing IDs and proxy passwords are generated on first start. Retain schema version 1. Leave `corePath` omitted/empty to find the `core` directory beside the executable, so moving the bundle remains possible.

CLI options: `--data-dir PATH`, `--listen 127.0.0.1:PORT`, `--core PATH`, `--no-browser`, `--version`. Overrides make isolated testing possible.

## Subscription behavior

Supports a single Xray configuration object or an array of configurations, up to 4 MiB / 512 entries. The catalog reports unsupported protocol/configuration shapes. Xray validates a configuration again before activation. Encrypted subscriptions, URI/base64 subscriptions, Clash YAML and sing-box JSON are not supported yet.

PortTide replaces provider listener/API/log settings and applies a proxy-only user-traffic route. Supported proxy transports, relay settings and balancers are preserved; provider per-site DIRECT/REJECT rules are not imported. Direct bootstrap DNS rules are restricted to internal DNS traffic. Direct balancer fallback and disabled TLS verification are rejected. Server-side routing performed by the VPN provider is outside the client's control.

A failed download or invalid update keeps the cached profiles. When an active profile disappears or becomes unsupported, refresh is refused until it is stopped. Changes to an active profile restart only PortTide's Xray process and may interrupt its existing connections. If startup fails, PortTide attempts to restore the previous runtime. A running process means its listener is ready, not that the remote server was verified.

## Use alongside Clash

Add a local SOCKS5 proxy with the address, port and credentials shown in PortTide, then select it in the relevant Clash group. Ensure the actual PortTide `core/xray.exe` path is excluded from interception when using a TUN client; otherwise a routing loop is possible. PortTide does not edit those rules. Nothing requires replacing your existing profiles or proxy groups.

## Development

Requires Go matching `go.mod`; Python 3.12+ is only used for packaging/core download. The UI is plain embedded HTML/CSS/JavaScript. No Node runtime is required by the shipped application.

```sh
python3 scripts/fetch-core.py --platform linux-amd64
PORTTIDE_TEST_CORE="$PWD/core/xray" go test -race ./...
go vet ./...
go run ./cmd/porttide --data-dir .local --no-browser
```

On Windows, fetch `--platform windows-amd64`, set `PORTTIDE_TEST_CORE` to `core/xray.exe`, and run `go test ./...`. Core-less unit tests run without that variable; integration tests explicitly report a skip.

```sh
python3 scripts/package.py
```

Packaging creates a Windows executable, pinned Xray assets, licenses and SHA-256 checksum under `dist/`. The original user data is never included. The build workflow runs native Windows tests and Linux race checks; it does not need a real subscription. Core version and archive checksums are recorded in [core-manifest.json](core-manifest.json).

See [implementation notes](docs/implementation.md) and [third-party notices](THIRD_PARTY_NOTICES.md). PortTide is MIT licensed.
