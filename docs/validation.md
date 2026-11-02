# Initial alpha validation — 2026-11-02

- Go unit/API tests and the Linux race detector passed.
- Native Windows x64 tests passed against the bundled Xray 26.10.31.
- Real local HTTP and SOCKS5 transfers passed on mixed and separate ports. Missing/wrong passwords were rejected.
- Occupied-port application rolled back to the prior runtime; canceled diagnostics left the active runtime unchanged.
- Force-terminating the owning process terminated its Xray child on both Linux and Windows.
- Geolocation and speed tests used controlled responses, covering conflicting databases, explicit-IP lookups, HTTP 429, byte limits, incomplete upload and no direct fallback.
- A private subscription was parsed and its 45 records validated by Xray without contacting the remote proxy servers. No subscription material was added to the repository.
- The actual Windows executable was inspected in Chrome through its browser extension: empty state, rejected HTTP URL, synthetic catalog, name search and diagnostic panel. The Windows desktop was not used for subsequent browser checks.
- The portable archive was inspected: executable, pinned core/data, documentation and licenses only. Private settings, caches and test data were excluded.

Live Internet latency/throughput/geography runs are not claimed by this validation. They were deliberately deferred while the user was gaming. Public diagnostic services may be unavailable or rate-limited; the application reports those failures.

The initial CI attempt exposed relative core paths resolved from the Go package directory. The failure was reproduced locally; the workflow now passes absolute core paths. Published tags are retained without rewriting history.

Corrected public CI passed on Windows and Linux: https://github.com/irtez/PortTide/actions/runs/41509115970 . Release v0.1.0-alpha.2 was published with a Windows x64 ZIP and SHA-256 checksum. Release code commit: 0e0d711c207a9dfe73d38259e514dc88189374d8.
