# PortTide — first working release

Status: completed for the initial alpha, 2026-11-02. Local and GitHub CI Windows/Linux checks passed. The portable v0.1.0-alpha.2 pre-release is published. Implementation authorized in the project discussion.

## Agreed product boundary

A Windows background application with a local browser interface. It manages proxy subscriptions and profiles and exposes authenticated loopback HTTP/SOCKS5 or mixed proxy listeners. It never configures system proxy, TUN, DNS, firewall, or routing. Other applications (including Clash) decide which traffic to send to it.

Users can supply and replace subscription URLs through the UI or a documented local settings file. The model supports multiple subscriptions. Initially support unencrypted Xray JSON objects/arrays, including the format used by Happ-compatible subscriptions. Unsupported formats/protocols are reported explicitly; encrypted subscription support is outside this release.

The catalog preserves names, emoji, available subscription notices and usage metadata. It distinguishes provider claims from measured results. Selection survives refresh/restart when identity is unchanged. Failed refresh preserves usable cached profiles. No personal subscription, server credential, or machine-specific network configuration belongs in this repository or release.

Latency, throughput and geography checks run through an isolated instance of the selected profile; they must not switch the active production profile. Geography distinguishes observer IPs from database lookups for the same IP. Speed tests are manual and bounded by bytes/time. Test errors must be visible, never replaced by invented results.

## Implementation choices

- Go backend using the standard library; browser UI embedded in the executable, with no browser build/runtime dependency for end users.
- External, version-pinned Xray process; no proxy protocol reimplementation. Prefer native mixed SOCKS/HTTP if protocol/authentication checks confirm it; allow separate ports.
- Bind UI and proxy only to loopback. HTTP API validates Host/Origin and uses a process-local access token. Subscription/configuration text is rendered as text, not HTML.
- Use a user data directory (LocalAppData on Windows), separate from the installed executable. Support --data-dir for isolated development/testing.
- Import a controlled subset of Xray configuration. Replace inbound/listener/API/logging settings. Preserve supported transport/relay/balancer structure; never silently permit provider user-traffic DIRECT rules or DIRECT fallback. Bootstrap DNS may go direct through an explicitly isolated internal DNS route.
- Start/stop only processes owned by this application. Validate candidates, serialize transitions, and restore the previous runtime if application fails. UI distinguishes process readiness from network verification.
- MIT for PortTide. Preserve third-party license notices and link corresponding Xray source versions in distributable packages.

## Steps and evidence

1. Configuration, catalog and subscription importer: atomic writes, source editing, bounded downloads, stable profile IDs, metadata, unsupported-profile explanations. Tests cover changed source order, invalid/oversized input, duplicate names and retaining cached profiles on error.
2. Xray configuration compiler and runtime: mixed/separate authenticated listeners, selected route, preserved safe balancing, isolated tests, start/stop and rollback. Tests cover direct fallback rejection, inbound replacement, unsupported configurations and real HTTP/SOCKS authentication against a local test destination.
3. Diagnostics: HTTPS latency, bounded download/upload, multiple IP observers and explicit-IP geolocation lookups. Tests use controlled local endpoints and cover failure/cancellation without modifying the active selection.
4. Local API and embedded UI: subscriptions, profile search/selection, proxy settings, diagnostics/results and runtime state. Verify access protection, request validation, escaping of imported content and the complete browser workflow.
5. Windows packaging and documentation: clean portable bundle, core version/checksum manifest, third-party notices, startup/shutdown instructions, proxy client examples and CI. Build Windows x64 and run a real isolated Windows smoke test before claiming Windows support.

## Completion criteria

A clean user can launch a release, add a supported subscription, inspect names/metadata, select a supported profile, connect through HTTP and SOCKS5 with configured authentication, run bounded diagnostics, and recover the same selection after restart. Failed updates/transitions preserve the previous usable state. No live user routing is changed by installation or testing. Publish only after targeted tests, Windows build/smoke test, browser inspection and a diff/secret review.

## Next action

Collect feedback on https://github.com/irtez/PortTide/releases/tag/v0.1.0-alpha.2 . Live upstream throughput and geography checks remain an explicit follow-up; they were not run during the user’s gaming session. Do not connect or replace an existing daily proxy automatically.

## UI and diagnostics refinement — 2026-11-03 (local preview; no release requested)

Remove the slogan and the large connection prompt after subscription setup. Keep connection/stop in a compact header, move address copying into proxy settings, and preserve every existing action. Show provider announcement/emoji legend and quota/expiry immediately below the Profiles title. Render country flags using bundled SVG assets. Open diagnostics in a side panel rather than scrolling past the whole catalog. Improve latency with warmup and seven samples; use persistent connections, four parallel adaptive transfers, excluded warmup, 20-second measurement windows and a strict 1 GiB payload budget per direction. Report progress and limits honestly; do not change Clash routing. Browser-extension UI automation is allowed; desktop-wide control is not.

User requested holding a new release while further improvements are discussed. Work stays in a local development checkpoint; do not tag, publish, package a release or replace the installed alpha.2 yet. Preview uses a separate port and private copy of data. Full Desktop Computer Use remains disabled; browser extension control is allowed.

Port form refinement: Mixed displays only the shared HTTP/SOCKS5 port. Switching to separate reuses it as SOCKS5 and reveals the independently retained HTTP port; switching back hides HTTP without clearing it. New settings leave the HTTP port unset (0 in JSON, blank in the UI); existing saved values are preserved.

## Windows lifecycle and distribution

See [Windows daily-use implementation](windows-lifecycle.md) for the accepted behavior and verification. Settings schema 2 separates desktop preferences from session Stop. Per-data-directory locks and authenticated local handoff prevent duplicate cores; a native Win32 tray opens the existing web UI. Inno Setup installs per-user at a stable path; an update resume marker preserves pause without changing next-launch preferences. GitHub installer downloads require the official repository URL, advertised size and SHA-256 before execution. The current version number is intentionally unchanged pending release approval.
