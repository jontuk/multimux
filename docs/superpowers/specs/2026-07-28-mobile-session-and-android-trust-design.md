# Mobile session view and Android CA trust — design

Date: 2026-07-28

## Goal

Make multimux useful from an Android phone for checking running sessions and
issuing occasional short commands.

The phone experience shows one live terminal at a time in portrait. The user
swipes a compact session header to move through running sessions. This mobile
selection never changes the persisted desktop grid.

The same change also closes the Android onboarding gap: a phone must trust the
daemon's local CA before Chrome treats the origin as secure and WebAuthn can
register or use a passkey.

## Scope

In scope:

- Android Chrome and the installed Android PWA, portrait first.
- A responsive single-session view on viewports at or below 560 CSS pixels.
- Running sessions already known to the configured multimux daemons.
- A public, read-only export of the daemon's CA certificate.
- A guided Android CA-installation page before setup or login.
- Small responsive changes needed to keep Settings reachable.

Out of scope:

- Sustained phone-first terminal work or a native Android app.
- Launching, renaming, moving, removing, or terminating sessions from the
  mobile session view.
- Changing the desktop layout or its column count from a phone.
- A comprehensive mobile redesign of every Settings panel.
- iPhone/iPad-specific onboarding.
- Bypassing managed-device policy that forbids user-installed CAs.
- Externally issued certificates or a configurable external TLS terminator.
- A new plaintext bootstrap listener.

Tablets and viewports wider than 560 CSS pixels keep the current grid.

## Platform findings

Android supports PEM and DER CA certificates, and current Chrome considers
user-added TLS certificates from the platform trust store by default. Android
11 and later require the user to finish CA installation in system Settings; a
web app cannot silently grant this trust.

References:

- [Android network security configuration](https://developer.android.com/privacy-and-security/security-config)
- [Google's Android 11+ CA installation flow](https://support.google.com/device-usage-study-help/answer/15713321?co=GENIE.Platform%3DAndroid&hl=en)
- [Chrome user-added TLS certificate policy](https://support.google.com/chrome/a/answer/2657289?hl=en-EN)
- [Chrome Root Store and local trust FAQ](https://chromium.googlesource.com/chromium/src/+/main/net/data/ssl/chrome_root_store/faq.md)

Multimux already writes a PEM-only, name-constrained CA at `pki/ca.pem`. The
missing capabilities are a phone-friendly way to obtain that public
certificate and guidance through Android's manual trust step.

## Mobile session experience

### Responsive boundary

`GridPage` observes `(max-width: 560px)` with a small `matchMedia` hook. It
renders one of two branches:

- Wide: the existing header controls and grid, unchanged.
- Narrow: `MobileSessionView`, with no launcher, column stepper, unplaced
  quick-add buttons, empty grid cells, or tile actions.

The breakpoint is based on viewport width, not user-agent detection. A narrow
desktop window may therefore show the mobile view, which is desirable
responsive behavior. A phone rotated to a viewport wider than 560 pixels gets
the existing grid; portrait is the supported phone orientation for this pass.

Crossing the breakpoint unmounts one rendering branch and mounts the other.
It does not call `persist` or write `/api/layout`.

### Session ordering

Session ordering is a pure helper with these inputs:

- normalized desktop layout tiles;
- configured servers in browser-local server order;
- the latest session list for each server.

It returns running sessions only:

1. Running sessions that occupy a tile, in normalized `layout.tiles` order.
2. Every remaining running session, grouped by configured-server order and in
   the order returned by that server's `/api/sessions`.

Each entry is keyed by `serverId:sessionId`. Duplicate placed references are
ignored defensively. Tiles whose server was removed and session rows that are
not running do not enter the mobile list.

This ordering gives the desktop grid priority without treating the mobile view
as another layout. Swiping into the unplaced portion never attaches a session
to a grid cell.

### Selection lifecycle

`MobileSessionView` owns an ephemeral selected session key.

- On first usable data, select the first ordered session.
- When data refreshes, retain the selected key if it still exists.
- If the selected session disappears, retain its old numeric position in the
  new list. This selects the next item when one exists, or the previous item
  when the removed session was last.
- If no running sessions remain, clear selection and show the empty state.
- Mobile selection is not stored in SQLite or `localStorage`.

Initial loading is separate from an empty list. It ends after the layout request
and every configured server's initial session request have each settled,
whether successfully or with an error. A failed server contributes no sessions
and remains represented by its status banner. The empty state must not flash
while any of those initial requests are unresolved.

Only the selected `TerminalTile` is mounted. Changing sessions disposes its
xterm/WebSocket connection and mounts the next one; the tmux session continues
uninterrupted. Keeping hidden terminals mounted was rejected because it would
consume phone resources and let hidden connections participate in terminal
resize arbitration.

### Compact session header

The mobile page contains:

1. A compact app bar with the multimux wordmark and an accessible Settings
   control.
2. A single-line session header.
3. The selected terminal filling the remaining viewport.

The session header contains:

- `#id · label`, using the existing label/tool/tmux-name fallback;
- git branch/tracking and directory context when available;
- a fixed trailing position such as `2/5`.

Metadata truncates before the session title, and the title then ellipsizes if
the viewport is still too narrow. There are no arrows and no instructional
"swipe" copy.

The text Grid/Settings navigation collapses on narrow grid view to one Settings
icon with an accessible label. The wordmark remains a link to the grid. Settings
must not create global horizontal page overflow; a wide settings table may
scroll within its tab content. The individual Settings panels otherwise keep
their current design.

### Swipe gesture

Only the session header is a navigation surface. The terminal continues to
receive its existing touch, mouse-mode, keyboard, and selection events.

The header uses Pointer Events:

- Record the primary pointer's start position.
- On pointer-up, accept a swipe when horizontal travel is at least 48 CSS
  pixels and exceeds vertical travel.
- Negative horizontal travel selects the next session; positive travel selects
  the previous session.
- Clamp at the first and last entries; do not wrap.
- Pointer cancellation performs no navigation.

The header uses `touch-action: pan-y`, so vertical browser gestures remain
available while multimux handles deliberate horizontal swipes.

### Viewport and keyboard

The mobile terminal fills the space below the two compact bars using dynamic
viewport units and Android safe-area insets. It must not retain the desktop
`calc(100vh - 60px)` assumption.

The existing `ResizeObserver` in `TerminalTile` refits xterm when Chrome's
visible viewport or the software keyboard changes the container size. Opening
and closing the keyboard must not leave stale terminal rows or content hidden
behind browser/PWA chrome.

### Empty and error states

With no running sessions, the mobile view says that no sessions are running and
that launching is available from a wider device in this first pass.

Existing `TerminalTile` overlays continue to handle reconnecting, ended,
missing, and unauthenticated selected sessions. Existing per-server status
banners remain above the mobile terminal. An unreachable non-selected server
does not block use of sessions from reachable servers.

## CA export

### Startup ordering and shared inspection

`pki.Ensure` moves before `server.New`, so the HTTP server always receives a
valid CA provider. The setup banner remains after `Ensure`; this lets it print
the CA fingerprint alongside the setup and Android trust URLs.

CA parsing and description become a shared PKI helper rather than parallel
implementations in `cmd` and `internal/server`. Inspection:

- accepts exactly one PEM `CERTIFICATE` block plus surrounding whitespace;
- rejects malformed data, additional PEM blocks, and non-CA certificates;
- returns the subject common name, permitted DNS domains, RFC3339 UTC expiry,
  and the uppercase colon-separated SHA-256 fingerprint of the certificate's
  DER bytes.

The existing `ca trust` output, first-run setup banner, and CA-regeneration
warning use this same inspection result. The fingerprint printed by the daemon
is the user's out-of-browser reference.

`server.Config` receives a read-only function that returns the current CA
bytes. The server reads and inspects the file on every export/info request so a
daily CA regeneration becomes visible immediately without another server
restart or copied certificate state.

### Public endpoints

Add two unauthenticated, read-only, non-API routes:

`GET /ca.crt`

- Returns the inspected PEM certificate and no other file contents.
- `Content-Type: application/x-x509-ca-cert`
- `Content-Disposition: attachment; filename="multimux-ca.crt"`
- `Cache-Control: no-store`
- `X-Content-Type-Options: nosniff`

`GET /ca/info`

- Returns JSON with `subject`, `permittedDNSDomains`, `expires`, and
  `sha256Fingerprint`.
- Uses `Cache-Control: no-store` and `X-Content-Type-Options: nosniff`.

The routes are available while setup is pending, after setup, and without a
login. A CA certificate is public material; `ca.key`, `key.pem`, database
contents, setup codes, and all other PKI files remain unreachable.

Missing, unreadable, malformed, multi-certificate, or non-CA content returns an
internal error and no certificate bytes. The download does not fall through to
the SPA.

## Android trust experience

### Entry points

Add a dedicated public `/trust` SPA route. It is linked from:

- the first-run terminal banner, using the primary displayable origin;
- setup and login screens when `window.isSecureContext` is false;
- README Android/mobile instructions;
- the re-trust warning printed after CA regeneration.

The terminal banner also prints the full SHA-256 CA fingerprint.

Opening the daemon before trust still produces Chrome's certificate warning.
The user proceeds past it once on a network they control, then reaches the
guided trust page. No extra HTTP listener is introduced.

### Trust page

The page fetches `/ca/info` and shows:

- why Android trust is required for passkeys and the PWA;
- the CA subject, constrained hostnames, expiry, and SHA-256 fingerprint;
- a prominent `Download CA certificate` link to `/ca.crt`;
- stock Android/Pixel installation steps;
- a note that Samsung and other OEM wording differs;
- a reload/check action after installation;
- a managed-device explanation and out-of-band fallback.

The primary Android path is:

1. Download `multimux-ca.crt`.
2. Open Settings → Security & privacy → More security settings →
   Encryption & credentials → Install a certificate → CA certificate.
3. Select the downloaded file and approve Android's CA warning.
4. Return to Chrome and reload.

For Samsung, documentation uses its equivalent Security and privacy → More
security settings → Install from device storage path.

`window.isSecureContext` is the completion signal:

- False: keep showing download/install guidance.
- True after reload: show success and continue to the original destination.

Setup and login share a small trust prompt rather than rendering passkey
controls that cannot work in an insecure context. The dedicated trust page also
supports adding a phone after initial daemon setup.

### Return path

The trust page may carry a return target for the current setup or login URL,
including the setup code query. Before navigation, parse it against
`window.location.origin` and accept it only when the resolved origin matches
exactly. Protocol-relative and cross-origin targets are rejected. The fallback
destination is `/`.

The trust page never sends the return target to another origin.

### Bootstrap security

The initial page and certificate download are reached only after bypassing an
untrusted-certificate warning. Therefore, the fingerprint displayed by that
page is informative but is not itself an authenticated reference.

The page states all of the following:

- Use this guided download only on a LAN, VPN, or tailnet the user controls,
  matching multimux's existing security model.
- Compare the installed certificate's SHA-256 fingerprint with the value
  printed by the daemon, not merely with the initially untrusted web page.
- Do not continue on a mismatch.
- If the network is not fully controlled, transfer `pki/ca.pem` through an
  already trusted channel such as SSH to a computer followed by USB or Quick
  Share to the phone.

The existing critical DNS name constraints materially limit the trusted CA to
the configured daemon names, but do not remove the need to authenticate the
initial certificate transfer.

If device policy blocks user-added CAs or Chrome blocks proceeding to the
untrusted page, multimux explains the limitation. It does not weaken TLS,
disable certificate validation, or attempt to bypass device management.

## Data flow

Mobile session flow:

1. `GridPage` loads layout, server sessions/tools, and event status as today.
2. The ordering helper derives the running mobile list without mutation.
3. `MobileSessionView` retains or repairs its ephemeral selected key.
4. One `TerminalTile` connects to the selected daemon/session.
5. A valid header swipe changes the key and remounts that one terminal.

Android trust flow:

1. The user opens setup, login, or `/trust` and bypasses Chrome's warning once.
2. An insecure context shows trust guidance instead of passkey controls.
3. `/ca/info` describes the current CA and `/ca.crt` downloads it.
4. Android Settings installs the CA into the user trust store.
5. Reload establishes a normally verified TLS connection.
6. `window.isSecureContext` becomes true and the user continues to setup/login.

## Testing

Implement test-first.

### PKI and server tests

- Inspect a valid CA and assert subject, constraints, expiry, and exact SHA-256
  fingerprint formatting.
- Reject a leaf, malformed PEM, extra PEM block, and trailing non-whitespace.
- `GET /ca.crt` succeeds before and after setup and without authentication.
- Certificate response body is exactly the inspected public CA.
- Download headers are exact and include `no-store` and `nosniff`.
- `GET /ca/info` matches the downloaded certificate.
- Missing or invalid CA provider data fails without leaking partial contents.
- Server responses never contain CA or leaf private-key material.
- Setup and regeneration banner tests pin the fingerprint and trust URL.

### Web unit/component tests

- The ordering helper puts placed running sessions first and appends unplaced
  running sessions in stable server/session order.
- It excludes stopped sessions, removed-server tiles, and duplicate references.
- A narrow viewport renders one terminal and no launcher, stepper, quick-add,
  empty cells, or tile actions.
- A wide viewport retains the existing grid and header controls.
- Horizontal swipe above threshold changes in the correct direction.
- Short, vertical, cancelled, first-boundary, and last-boundary gestures do not
  change selection.
- Swiping performs no layout `PUT`.
- Live removal retains selection, selects next, selects previous at the end,
  and reaches the empty state correctly.
- Initial loading does not flash the empty state.
- Insecure setup/login renders the shared trust prompt instead of passkey
  controls.
- `/trust` renders CA metadata, download link, and Android steps.
- Secure-context reload exposes the continue action.
- Same-origin return paths survive; protocol-relative and cross-origin values
  are rejected.

### Manual acceptance

Use current Android Chrome on a real device or emulator:

- First visit and certificate-warning bypass on a controlled network.
- CA download and stock Android/Pixel installation.
- Samsung-path wording check where a Samsung device is available.
- Fingerprint comparison against daemon output.
- Reload into a secure context and complete passkey registration/login.
- Install and launch the PWA.
- Portrait terminal fit before, during, and after software-keyboard use.
- Swipe through placed then unplaced sessions without desktop layout changes.
- Session disappearance, daemon reconnect, and zero-session state.
- Tablet/wide viewport retains the desktop grid.

Run `./verify.sh`; all existing and new checks must pass before implementation
is complete.

## Documentation

Update README:

- Change the v1 phone non-goal to distinguish "no native phone app" from the
  supported focused Android PWA experience.
- Add Android CA download/install/verify/remove instructions.
- Explain the initial Chrome warning and controlled-network requirement.
- Document the out-of-band transfer alternative.
- Explain that CA regeneration requires repeating trust on every phone.
- Describe the one-session swipe view, placed-then-unplaced order, narrow
  breakpoint, hidden launcher/actions, and tablet behavior.

No database migration, backend session API change, or persisted mobile
preference is required.
