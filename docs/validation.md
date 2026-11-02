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

## Local refinement validation — 2026-11-03

- New timing/budget/parallelism tests, Linux race tests and native Windows tests passed.
- SVG flags and the provider announcement/usage are visible in Chrome. A diagnostic drawer opened and closed at the end of a 49-record catalog with unchanged scroll offset (2935.2 px).
- A user-authorized live speed check exposed HTTP 403 on a 16 MiB Cloudflare download request, while 4 MiB requests succeeded. The transfer cap was reduced to 4 MiB, and a regression test now rejects a result when all measurement streams fail.
- A focused corrected download completed with zero failures: 546.27 Mbit/s over 14.74 seconds of measurement, ending at the 1 GiB payload cap (warmup included in that cap). Upload in the preceding live test completed its full 20-second window at 34.52 Mbit/s. These are observations through the selected profile, not guarantees or provider-independent benchmarks.
- No release artifact or tag is made for this refinement at the user's request. The installed alpha.2 and existing Clash routing stay unchanged.

- Live warmed HTTPS latency completed 7/7 samples: median 40.24 ms, range 39.16–41.14 ms, adjacent-sample variation 0.81 ms (same explicit test profile).
- Browser checks confirmed that Mixed exposes only the shared port; switching to separate retains that value as SOCKS5 and restores HTTP. Changing the shared port and switching back retained the independent HTTP value. An empty HTTP value stayed empty. Test edits were canceled rather than applied.
- The moved stop action was exercised on the isolated preview listener. Address copying remains accessible in proxy settings; its existing handler is preserved. The real subscription link was restored in the private preview after the user noticed the disabled refresh button; refresh succeeded with 49 entries.
