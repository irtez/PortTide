# Third-party components

PortTide source is licensed under MIT. Its licenses do not replace the licenses of bundled components.

- **Xray-core 26.10.31** — Mozilla Public License 2.0. The bundle includes `core/LICENSE` and `core/SOURCE.txt`. Corresponding source: https://github.com/XTLS/Xray-core/tree/v26.10.31 . Unmodified official release binaries and the associated geoip/geosite asset files are obtained from the pinned, checksum-verified Xray release. The runtime assets retain their upstream terms. Upstream repository: https://github.com/XTLS/Xray-core .
- **Go runtime and standard library** — BSD-style Go license, included in `third_party/GO-LICENSE` in the portable bundle. Source: https://go.dev/ .

The UI uses system fonts. PortTide does not bundle browser frameworks, the Cloudflare speedtest JavaScript package, or Wintun.

Diagnostics make HTTPS requests to Google gstatic (latency), Cloudflare (throughput and exit observation), ipify (exit observation), ipwho.is and ipapi.co (explicit-IP geographic lookups). These external services see the proxy exit address and can have their own availability and rate limits. No diagnostic results are uploaded by PortTide to a separate analytics backend.

- **flag-icons 7.6.0** — MIT, Copyright (c) 2013 Panayiotis Lipiridis. Country SVGs are bundled locally; license and source manifest: `internal/app/web/flags/LICENSE` and `SOURCE.txt` (also served locally at `/flags/LICENSE` and `/flags/SOURCE.txt`; packaged license: `third_party/FLAG-ICONS-LICENSE`). Source: https://github.com/lipis/flag-icons .
