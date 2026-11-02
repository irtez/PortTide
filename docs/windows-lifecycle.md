# Windows daily-use implementation — 2026-11-03

Status: implemented locally; final validation below. No new public release authorized.

## Agreed behavior

- Per-user installation at a stable path; data remain in LOCALAPPDATA/PortTide.
- Native tray + existing browser UI; one instance per data directory.
- Background mode defaults on; startup at logon respects it. Explicit shortcut/tray Open always opens UI. First interactive launch opens setup.
- Independent preferences: launch at Windows logon, connect on app launch, background launch. Stop affects this session only. A normal next launch connects again when enabled.
- Last profile, otherwise first supported in the same source, otherwise first supported in source order. No name matching. Network failure never selects another country.
- Cached startup precedes background source refresh. No configured sources leaves app stopped.
- Update restart preserves running/stopped state and stays quiet. Settings, authentication and custom core path survive.
- Tray: Open, status/profile, Start/Stop, Settings, Exit. Closing browser does not exit.
- Notices only for actionable errors/update availability, deduplicated. UI reconnects after restart.
- Proxy-only; external applications choose traffic and filtering. No changes to production Clash/system proxy/TUN.

## Work and checks

1. Lifecycle/settings: add desktop preferences with explicit migration from legacy autoStart, stable source selection, start/stop helpers and update-resume state. Test migration, fallback order, stopped session vs next boot, update resume.
2. Single instance/Windows host: process-owned data lock and authenticated local handoff; hidden native window, tray and user Run-key integration. Test lock lifetime/handoff/native struct sizes and registry round trip with an isolated test value. Smoke launch in isolated temp data without desktop control.
3. App settings/UI: separate application settings from proxy settings; background/logon/connect, core/refresh, updates, exit; connection recovery with refreshed API token. Validate JS and browser-only interaction.
4. Distribution/update: per-user Inno Setup at stable path, Start/Desktop shortcuts using --open, install/upgrade/uninstall coordination, optional user-data removal. GitHub release discovery, semantic version ordering, strict official asset URLs/digest, staged download before installer launch. Preserve paused state through installer restart; installer failure must retain recoverable old settings. Unit tests with local fake releases/downloads; compile installer locally only if tooling available. No publication or version bump.
5. General README and validation notes; full Linux race/vet and native Windows tests; inspect diff/secrets; local scoped commits.

## File boundaries

- model.go, lifecycle.go: persisted preferences, selection and lifecycle policy.
- instance.go + lock OS files: single-instance lock and authenticated commands.
- desktop_windows.go / desktop_other.go: native tray and logon registration.
- updates.go + updates tests: discovery/download/validation/update handoff.
- server.go + cmd main: API integration and process orchestration.
- web/index.html, app.js, style.css: settings, updates, reconnect UX.
- scripts/installer.iss + build script/CI: stable installation and upgrade.

## Validation boundaries

Windows native UI automation is not authorized; inspect via APIs/process state and browser extension only. Test autostart under a temporary registry value; do not silently register the development preview as the user's production startup. No new real throughput test needed. No push/tag/release while release hold remains.

## Observed validation (2026-11-03)

- Linux integration suite with Xray and race detector passed; Go vet and JS syntax passed.
- Native Windows suite passed: Win32 structure layouts, tray creation/removal, temporary Run-key round trip, instance lock/handoff, migration, lifecycle, downloader checks and existing Xray tests.
- Inno Setup 6.7.4 obtained from its official immutable release, verified by SHA-256 and valid Pyrsys B.V. Authenticode signature. Extracted in portable mode into Temp.
- Only an isolated test installer was compiled; separate AppId/data/UI port/shortcuts. Production PortTide installation, startup entry and Clash were not changed.
- Fresh install returned 0. Synthetic VLESS profile pointed at loopback port 9, never a VPN server. Its local proxy started on test port 17994.
- Upgrade while running returned 0, replaced the old process, restored running state, selection and credentials. A settings backup was present.
- Upgrade after Stop returned 0 and retained the paused state with connect-on-launch still enabled. A subsequent normal background launch connected again.
- Duplicate background launch exited 0 and retained the existing instance. Silent uninstall exited 0, removed executable/owned processes/test shortcut, and preserved test user data.
- A PowerShell harness initially used Start-Process -Wait, which waits descendants including the relaunched app. The installer had already succeeded. Subsequent checks use Process.WaitForExit for the installer only.
- Chrome extension verified the separate Application dialog, default background setting, save/reopen, unchanged proxy settings and a real GitHub version check.
- The already-open browser tab recovered automatically after a prepare-update/resume cycle, including API-token renewal, with all 49 preview profiles intact. No tab reload was used for recovery.

Remaining release work: choose a new version and release notes, publish only when the user lifts the release hold. The full network download-to-new-public-release path cannot be exercised until an installer release exists; download integrity/error cases and actual installer upgrade were verified separately. Windows logoff/reboot and interactive tray menu clicks were not automated; global desktop control remains disallowed.
