# Mobile Session View and Android CA Trust Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a portrait-first, read-only mobile session switcher and a secure Android onboarding path for downloading, verifying, and installing the daemon's local CA.

**Architecture:** The Go PKI package becomes the single CA-inspection authority, and the server exposes that public certificate through two narrowly scoped non-API routes backed by a per-request file reader. The React grid keeps its existing data ownership but selects a separate branch at 560 CSS pixels: the desktop branch remains unchanged, while the mobile branch orders all running sessions without persisting selection or layout. A public trust route and shared insecure-context prompt guide Android users back to setup or login through a same-origin-only return target.

**Tech Stack:** Go (`crypto/x509`, `encoding/pem`, `net/http`), React 19 + TypeScript, CSS Pointer Events and dynamic viewport units, Vitest + Testing Library, Go tests.

## Global Constraints

- Source spec: `docs/superpowers/specs/2026-07-28-mobile-session-and-android-trust-design.md`.
- The mobile breakpoint is exactly `(max-width: 560px)` and is based only on viewport width.
- Mobile selection is ephemeral. Do not write it to SQLite, `localStorage`, or `/api/layout`.
- Only one mobile `TerminalTile` may be mounted at a time.
- `/ca.crt` and `/ca/info` are public read-only routes. No other file under `pki/` becomes addressable.
- Read and validate the current CA on every CA request; do not cache certificate bytes in `server.Config`.
- CA parsing accepts exactly one PEM `CERTIFICATE` block plus surrounding whitespace.
- Keep the existing `TerminalTile` `ResizeObserver`; the mobile container must resize correctly instead of adding a second terminal-resize mechanism.
- Fix all compiler, vet, lint, formatting, test, and build failures as they appear. `./verify.sh` must pass at the end.

## File Map

- Create `internal/pki/inspect.go`: strict public CA parsing and shared description type.
- Create `internal/pki/inspect_test.go`: parser, fingerprint, and rejection tests.
- Create `internal/server/ca.go`: `/ca.crt` and `/ca/info` handlers.
- Create `internal/server/ca_test.go`: public-route, header, refresh, and failure tests.
- Modify `internal/server/server.go`: add the CA reader to `Config` and register the two routes.
- Modify `internal/server/server_test.go`: give test servers a valid CA reader.
- Modify `cmd/ca.go`: replace command-local X.509 parsing with the PKI helper.
- Modify `cmd/ca_test.go`: assert the shared fingerprint and strict validation.
- Modify `cmd/serve.go`: ensure PKI before server creation and enrich startup/regeneration output.
- Modify `cmd/serve_test.go`: cover setup/trust URLs, fingerprint output, and regeneration guidance.
- Create `web/src/useMediaQuery.ts`: lifecycle-safe `matchMedia` hook.
- Create `web/src/grid/mobileModel.ts`: pure mobile ordering and selection reconciliation.
- Create `web/src/__tests__/mobile-model.test.ts`: ordering and selection lifecycle tests.
- Create `web/src/grid/SessionMetadata.tsx`: title fallback and reusable git metadata.
- Create `web/src/grid/MobileSessionView.tsx`: one-terminal mobile UI and header swipe handling.
- Create `web/src/__tests__/mobile-session-view.test.tsx`: loading, empty, metadata, swipe, and mount-count tests.
- Modify `web/src/grid/GridPage.tsx`: track initial request settlement and choose desktop/mobile branches.
- Modify `web/src/__tests__/grid-page.test.tsx`: breakpoint, no-persist, loading, and server-error integration coverage.
- Create `web/src/pages/TrustPage.tsx`: public Android CA installation guide.
- Create `web/src/pages/TrustPrompt.tsx`: shared insecure-context setup/login prompt.
- Create `web/src/__tests__/trust.test.tsx`: trust data, completion, and return-target security tests.
- Modify `web/src/App.tsx`, `web/src/pages/SetupPage.tsx`, and `web/src/pages/LoginPage.tsx`: route and prompt integration.
- Modify `web/src/__tests__/startup.test.tsx` and `web/src/__tests__/login.test.tsx`: public route and insecure-context behavior.
- Modify `web/src/index.css`: compact shell, mobile terminal sizing, safe areas, truncation, and contained settings overflow.
- Modify `README.md`: Android trust/bootstrap and mobile-view instructions.

---

### Task 1: Strict shared CA inspection

**Files:**
- Create: `internal/pki/inspect.go`
- Create: `internal/pki/inspect_test.go`

**Produces:**
- `pki.CAInfo`
- `pki.InspectCA(raw []byte) (CAInfo, error)`
- `pki.FormatCAInfo(info CAInfo) string`

- [ ] **Step 1: Write the failing parser tests**

Create `internal/pki/inspect_test.go` with a test certificate helper and table tests covering surrounding whitespace, subject, ordered DNS constraints, UTC RFC3339 expiry, uppercase colon-separated SHA-256, malformed PEM, wrong PEM type, trailing non-whitespace, a second PEM block, and a non-CA certificate:

```go
package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"slices"
	"strings"
	"testing"
	"time"
)

func inspectionPEM(t *testing.T, isCA bool) ([]byte, []byte, time.Time) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	expiry := time.Date(2036, 7, 28, 12, 34, 56, 0, time.FixedZone("offset", 3600))
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: "multimux local CA (phone)"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: expiry,
		IsCA: isCA, BasicConstraintsValid: true,
		PermittedDNSDomains: []string{"phone.local", "phone.example.ts.net"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), der, expiry
}

func TestInspectCA(t *testing.T) {
	raw, der, expiry := inspectionPEM(t, true)
	info, err := InspectCA(append(append([]byte("\n\t"), raw...), []byte(" \n")...))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(der)
	plain := strings.ToUpper(hex.EncodeToString(sum[:]))
	var pairs []string
	for len(plain) > 0 {
		pairs, plain = append(pairs, plain[:2]), plain[2:]
	}
	wantFingerprint := strings.Join(pairs, ":")
	if info.Subject != "multimux local CA (phone)" ||
		!slices.Equal(info.PermittedDNSDomains, []string{"phone.local", "phone.example.ts.net"}) ||
		info.Expires != expiry.UTC().Format(time.RFC3339) ||
		info.SHA256Fingerprint != wantFingerprint {
		t.Fatalf("InspectCA = %+v", info)
	}
}

func TestInspectCARejectsAnythingExceptOneCACertificate(t *testing.T) {
	ca, _, _ := inspectionPEM(t, true)
	leaf, _, _ := inspectionPEM(t, false)
	cases := map[string][]byte{
		"empty": nil, "malformed": []byte("not pem"),
		"wrong type": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("x")}),
		"leading text": append([]byte("secret\n"), ca...),
		"trailing text": append(append([]byte{}, ca...), []byte("secret")...),
		"second block": append(append([]byte{}, ca...), ca...),
		"non CA": leaf,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := InspectCA(raw); err == nil {
				t.Fatal("accepted invalid CA input")
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/pki/ -run InspectCA -v`

Expected: FAIL to compile because `InspectCA` does not exist.

- [ ] **Step 3: Implement strict inspection and shared formatting**

Create `internal/pki/inspect.go`:

```go
package pki

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"
)

type CAInfo struct {
	Subject             string   `json:"subject"`
	PermittedDNSDomains []string `json:"permittedDNSDomains"`
	Expires             string   `json:"expires"`
	SHA256Fingerprint   string   `json:"sha256Fingerprint"`
}

func InspectCA(raw []byte) (CAInfo, error) {
	trimmed := bytes.TrimSpace(raw)
	if !bytes.HasPrefix(trimmed, []byte("-----BEGIN CERTIFICATE-----")) {
		return CAInfo{}, errors.New("pki: CA must be one PEM CERTIFICATE block")
	}
	block, rest := pem.Decode(trimmed)
	if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
		return CAInfo{}, errors.New("pki: CA must be one PEM CERTIFICATE block")
	}
	if len(bytes.TrimSpace(rest)) != 0 {
		return CAInfo{}, errors.New("pki: CA PEM contains additional data")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return CAInfo{}, fmt.Errorf("pki: parse CA certificate: %w", err)
	}
	if !cert.IsCA {
		return CAInfo{}, errors.New("pki: certificate is not a CA")
	}
	sum := sha256.Sum256(cert.Raw)
	plain := strings.ToUpper(hex.EncodeToString(sum[:]))
	pairs := make([]string, 0, len(plain)/2)
	for len(plain) > 0 {
		pairs, plain = append(pairs, plain[:2]), plain[2:]
	}
	return CAInfo{
		Subject: cert.Subject.CommonName,
		PermittedDNSDomains: append([]string(nil), cert.PermittedDNSDomains...),
		Expires: cert.NotAfter.UTC().Format(time.RFC3339),
		SHA256Fingerprint: strings.Join(pairs, ":"),
	}, nil
}

func FormatCAInfo(info CAInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "CA: %s\n", info.Subject)
	if len(info.PermittedDNSDomains) > 0 {
		fmt.Fprintf(&b, "  constrained to: %s\n", strings.Join(info.PermittedDNSDomains, ", "))
	}
	fmt.Fprintf(&b, "  expires: %s\n", info.Expires)
	fmt.Fprintf(&b, "  SHA-256: %s\n", info.SHA256Fingerprint)
	return b.String()
}
```

- [ ] **Step 4: Run PKI tests**

Run: `go test ./internal/pki/ -v`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pki/inspect.go internal/pki/inspect_test.go
git commit -m "feat(pki): add strict shared CA inspection"
```

---

### Task 2: Public CA export and info routes

**Files:**
- Create: `internal/server/ca.go`
- Create: `internal/server/ca_test.go`
- Modify: `internal/server/server.go:20-31,52-96`
- Modify: `internal/server/server_test.go:22-49`

- [ ] **Step 1: Write failing public-route tests**

Create `internal/server/ca_test.go`. Use `inspectionPEM`-equivalent local test data or a small server-package certificate helper. Assert:

```go
func TestCAExportIsPublicDuringAndAfterSetup(t *testing.T) {
	for _, registered := range []bool{false, true} {
		t.Run(fmt.Sprint("registered=", registered), func(t *testing.T) {
			s, _, _ := newTestServer(t, registered)
			crt := do(t, s, "GET", "/ca.crt", "")
			if crt.Code != http.StatusOK ||
				crt.Header().Get("Content-Type") != "application/x-x509-ca-cert" ||
				crt.Header().Get("Content-Disposition") != `attachment; filename="multimux-ca.crt"` ||
				crt.Header().Get("Cache-Control") != "no-store" ||
				crt.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatalf("certificate response = %d, headers=%v", crt.Code, crt.Header())
			}
			info := do(t, s, "GET", "/ca/info", "")
			if info.Code != http.StatusOK || !strings.Contains(info.Body.String(), `"sha256Fingerprint"`) {
				t.Fatalf("info response = %d: %s", info.Code, info.Body.String())
			}
		})
	}
}
```

Add tests which mutate the closure's returned bytes between two requests, reject missing/read-error/malformed/multi-cert/non-CA data with 500 and no PEM certificate bytes, and verify `/ca.crt` does not serve SPA `index.html`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/server/ -run CA -v`

Expected: FAIL because the routes and `Config.ReadCA` do not exist.

- [ ] **Step 3: Add the reader and routes**

In `internal/server/server.go`, add:

```go
type Config struct {
	Store   *store.Store
	Auth    *auth.Manager
	Tmux    *tmuxmgr.Manager
	Arbiter *tmuxmgr.Arbiter
	WebFS   fs.FS
	Origins []string
	Version string
	ReadCA  func() ([]byte, error) // reads the current public CA; never a key
}
```

Register these before the SPA fallback:

```go
s.mux.HandleFunc("GET /ca.crt", s.handleCADownload)
s.mux.HandleFunc("GET /ca/info", s.handleCAInfo)
```

In `newTestServer`, create valid test material without duplicating certificate construction:

```go
testPKI := pki.New(t.TempDir())
if _, err := testPKI.Ensure([]string{"localhost"}); err != nil {
	t.Fatal(err)
}
caPEM, err := os.ReadFile(testPKI.CACertPath())
if err != nil {
	t.Fatal(err)
}
```

Add `os` and `internal/pki` to the test imports, then set:

```go
ReadCA: func() ([]byte, error) { return append([]byte(nil), caPEM...), nil },
```

- [ ] **Step 4: Implement the handlers**

Create `internal/server/ca.go`:

```go
package server

import (
	"bytes"
	"net/http"

	"github.com/jontuk/multimux/internal/pki"
)

func (s *Server) currentCA(w http.ResponseWriter) ([]byte, pki.CAInfo, bool) {
	if s.cfg.ReadCA == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "CA unavailable"})
		return nil, pki.CAInfo{}, false
	}
	raw, err := s.cfg.ReadCA()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "CA unavailable"})
		return nil, pki.CAInfo{}, false
	}
	info, err := pki.InspectCA(raw)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "CA unavailable"})
		return nil, pki.CAInfo{}, false
	}
	return append(bytes.TrimSpace(raw), '\n'), info, true
}

func noStoreCertificateHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func (s *Server) handleCADownload(w http.ResponseWriter, _ *http.Request) {
	noStoreCertificateHeaders(w)
	raw, _, ok := s.currentCA(w)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/x-x509-ca-cert")
	w.Header().Set("Content-Disposition", `attachment; filename="multimux-ca.crt"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (s *Server) handleCAInfo(w http.ResponseWriter, _ *http.Request) {
	noStoreCertificateHeaders(w)
	_, info, ok := s.currentCA(w)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, info)
}
```

- [ ] **Step 5: Run server tests**

Run: `go test ./internal/server/ -v`

Expected: PASS, including setup-gate and static-fallback tests.

- [ ] **Step 6: Commit**

```bash
git add internal/server/ca.go internal/server/ca_test.go internal/server/server.go internal/server/server_test.go
git commit -m "feat(server): expose public CA download and info"
```

---

### Task 3: Startup ordering and shared daemon CA output

**Files:**
- Modify: `cmd/ca.go:1-151`
- Modify: `cmd/ca_test.go:54-76`
- Modify: `cmd/serve.go:159-184,293-365`
- Modify: `cmd/serve_test.go:78-100`

- [ ] **Step 1: Update the command tests first**

Change `TestDescribeCA` into `TestDescribeCAUsesSharedInspection` and assert its output includes an uppercase colon-separated `SHA-256:` line. Add malformed trailing-data and second-certificate cases. Update setup-banner expectations to include:

```text
Android trust: https://mux.example.com:8686/trust?return=%2Fsetup%3Fcode%3DABC123
CA SHA-256: <fingerprint>
```

Add a regeneration-banner test expecting the reason, primary `/trust` URL, and the new fingerprint.

- [ ] **Step 2: Run the command tests to verify they fail**

Run: `go test ./cmd/ -run 'DescribeCA|SetupBanner|CARegenBanner' -v`

Expected: FAIL because banners do not accept CA metadata and command parsing is still permissive.

- [ ] **Step 3: Replace command-local parsing**

Remove `crypto/x509` and `encoding/pem` from `cmd/ca.go`. Replace `describeCA` with:

```go
func describeCA(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	info, err := pki.InspectCA(raw)
	if err != nil {
		return "", fmt.Errorf("ca trust: %s: %w", path, err)
	}
	return pki.FormatCAInfo(info), nil
}
```

- [ ] **Step 4: Enrich the banner helpers**

Use signatures:

```go
func setupBanner(display []string, code string, info pki.CAInfo) string
func caRegenBanner(reason pki.CARegen, primaryOrigin string, info pki.CAInfo) string
```

Build the trust return value with `url.QueryEscape("/setup?code="+code)`. Print only `display[0]` for the Android trust URL, print the full fingerprint, and keep the existing setup-origin list and hostname hint.

- [ ] **Step 5: Move PKI ensure before server creation**

In `runServe`, immediately after `auth.New` and before `server.New`:

```go
p := pki.New(filepath.Join(dir, "pki"))
regen, err := p.Ensure(names)
if err != nil {
	fmt.Fprintln(stderr, err)
	return 1
}
readCA := func() ([]byte, error) { return os.ReadFile(p.CACertPath()) }
caRaw, err := readCA()
if err != nil {
	fmt.Fprintln(stderr, err)
	return 1
}
caInfo, err := pki.InspectCA(caRaw)
if err != nil {
	fmt.Fprintln(stderr, err)
	return 1
}
```

Pass `ReadCA: readCA` to `server.Config`. Keep setup-code creation after `Ensure`, call the new banner helpers, and delete the old later `pki.New`/`Ensure` block. For daily regeneration, re-read and inspect the newly generated file before printing the warning so both `/ca/info` and console output reflect the new CA immediately.

- [ ] **Step 6: Run command and full Go tests**

Run: `go test ./cmd/ ./internal/pki/ ./internal/server/ -v`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd/ca.go cmd/ca_test.go cmd/serve.go cmd/serve_test.go
git commit -m "feat(cmd): print Android trust URL and CA fingerprint"
```

---

### Task 4: Pure mobile ordering and selection model

**Files:**
- Create: `web/src/grid/mobileModel.ts`
- Create: `web/src/__tests__/mobile-model.test.ts`

- [ ] **Step 1: Write failing model tests**

Test all ordering rules in one fixture: normalized placed order wins; duplicate placed references appear once; removed-server tiles, dead rows, and missing rows are absent; remaining sessions are grouped by configured-server order and retain API order. Add selection cases for first data, retained key, middle removal selecting the next item at the same index, last removal selecting the previous item, and empty data.

```ts
expect(orderMobileSessions(layout, [local, remote], byServer).map((x) => x.key)).toEqual([
  "remote:9",
  "local:2",
  "local:1",
  "remote:8",
]);
expect(reconcileMobileSelection({ key: "local:2", index: 1 }, withoutTwo)).toEqual({
  key: "remote:8",
  index: 1,
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && pnpm test src/__tests__/mobile-model.test.ts`

Expected: FAIL because `mobileModel.ts` does not exist.

- [ ] **Step 3: Implement the model**

Create:

```ts
export type MobileSession = {
  key: string;
  server: Server;
  session: Session;
};

export type MobileSelection = { key: string | null; index: number };

export function orderMobileSessions(
  layout: Layout,
  servers: Server[],
  sessionsByServer: Record<string, Session[]>,
): MobileSession[] {
  const serverById = new Map(servers.map((server) => [server.id, server]));
  const sessionByKey = new Map<string, MobileSession>();
  for (const server of servers) {
    for (const session of sessionsByServer[server.id] ?? []) {
      if (session.status === "running") {
        sessionByKey.set(`${server.id}:${session.id}`, { key: `${server.id}:${session.id}`, server, session });
      }
    }
  }
  const seen = new Set<string>();
  const result: MobileSession[] = [];
  for (const tile of layout.tiles) {
    if (!tile || !serverById.has(tile.serverId)) continue;
    const key = `${tile.serverId}:${tile.sessionId}`;
    const entry = sessionByKey.get(key);
    if (entry && !seen.has(key)) {
      seen.add(key);
      result.push(entry);
    }
  }
  for (const server of servers) {
    for (const session of sessionsByServer[server.id] ?? []) {
      const key = `${server.id}:${session.id}`;
      const entry = sessionByKey.get(key);
      if (entry && !seen.has(key)) {
        seen.add(key);
        result.push(entry);
      }
    }
  }
  return result;
}

export function reconcileMobileSelection(
  previous: MobileSelection,
  sessions: MobileSession[],
): MobileSelection {
  if (sessions.length === 0) return { key: null, index: 0 };
  const retained = sessions.findIndex((session) => session.key === previous.key);
  const index = retained >= 0 ? retained : Math.min(previous.index, sessions.length - 1);
  return { key: sessions[index].key, index };
}
```

- [ ] **Step 4: Run the model tests**

Run: `cd web && pnpm test src/__tests__/mobile-model.test.ts`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/grid/mobileModel.ts web/src/__tests__/mobile-model.test.ts
git commit -m "feat(web): define mobile session ordering and selection"
```

---

### Task 5: Mobile session view and swipe behavior

**Files:**
- Create: `web/src/grid/SessionMetadata.tsx`
- Create: `web/src/grid/MobileSessionView.tsx`
- Create: `web/src/__tests__/mobile-session-view.test.tsx`
- Modify: `web/src/grid/GridPage.tsx:37-91` (consume the extracted metadata helpers)

- [ ] **Step 1: Write failing component tests**

Mock `TerminalTile` and render `MobileSessionView` with ordered entries and `onRefresh={vi.fn()}`. Assert:

- unresolved initial data shows `Loading sessions…`, not the empty message;
- a settled empty list says no sessions are running and launching needs a wider device;
- exactly one terminal is mounted;
- `#id · label` uses label/tool/tmux fallback and position reads `2/5`;
- branch/tracking and directory context render when present;
- a primary horizontal pointer travel of `-48` or less moves next, positive moves previous;
- vertical-dominant, short, cancelled, and non-primary gestures do nothing;
- swipes clamp rather than wrap;
- changing selection unmounts the former terminal.

Use `fireEvent.pointerDown(header, { pointerId: 1, isPrimary: true, clientX: 100, clientY: 10 })` and matching `pointerUp`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && pnpm test src/__tests__/mobile-session-view.test.tsx`

Expected: FAIL because `MobileSessionView` does not exist.

- [ ] **Step 3: Implement selection and swipe**

The component accepts `sessions`, `toolsByServer`, `initialLoading`, and `onRefresh`. Keep `{key,index}` state, reconcile it in an effect whenever `sessions` changes, and use this exact gesture rule:

```ts
const pointerStart = useRef<{ id: number; x: number; y: number } | null>(null);

function onPointerDown(e: React.PointerEvent) {
  if (!e.isPrimary) return;
  pointerStart.current = { id: e.pointerId, x: e.clientX, y: e.clientY };
}

function onPointerUp(e: React.PointerEvent) {
  const start = pointerStart.current;
  pointerStart.current = null;
  if (!start || start.id !== e.pointerId) return;
  const dx = e.clientX - start.x;
  const dy = e.clientY - start.y;
  if (Math.abs(dx) < 48 || Math.abs(dx) <= Math.abs(dy)) return;
  setSelection((current) => {
    const index = Math.max(0, Math.min(sessions.length - 1, current.index + (dx < 0 ? 1 : -1)));
    return { key: sessions[index]?.key ?? null, index };
  });
}
```

Set `pointerStart.current = null` on pointer cancellation. Attach handlers only to `.mobile-session-header`. Move `sessionTitle`, `TrackingMarks`, and `gitStateTitles` unchanged from `GridPage.tsx` into `SessionMetadata.tsx`, export them there, and import them into both views; this avoids a `GridPage` ↔ `MobileSessionView` module cycle. Render the selected entry with:

```tsx
<div className="mobile-session-view">
  <div
    className="mobile-session-header"
    style={{ touchAction: "pan-y" }}
    onPointerDown={onPointerDown}
    onPointerUp={onPointerUp}
    onPointerCancel={() => {
      pointerStart.current = null;
    }}
  >
    <span className="mobile-session-title">
      #{selected.session.id} · {sessionTitle(toolsByServer[selected.server.id], selected.session)}
    </span>
    <span className="mobile-session-context">
      {selected.session.gitState && (
        <span className="mobile-session-branch">
          <span
            className={`git-dot git-dot-${selected.session.gitState}`}
            title={gitStateTitles[selected.session.gitState]}
          />
          <span className="tile-branch-name">{selected.session.branch}</span>
          <TrackingMarks session={selected.session} />
        </span>
      )}
      <span className="mobile-session-dir" title={selected.session.dir}>
        {selected.session.dir}
      </span>
    </span>
    <span className="mobile-session-position">{selection.index + 1}/{sessions.length}</span>
  </div>
  <div className="mobile-terminal">
    <TerminalTile
      key={selected.key}
      server={selected.server}
      sessionId={selected.session.id}
      onClose={onRefresh}
    />
  </div>
</div>
```

Do not render launcher, arrows, instructions, tile actions, or hidden terminals.

- [ ] **Step 4: Run component tests**

Run: `cd web && pnpm test src/__tests__/mobile-session-view.test.tsx`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/grid/SessionMetadata.tsx web/src/grid/MobileSessionView.tsx web/src/__tests__/mobile-session-view.test.tsx web/src/grid/GridPage.tsx
git commit -m "feat(web): add single-terminal mobile session view"
```

---

### Task 6: Responsive branch and initial-request settlement

**Files:**
- Create: `web/src/useMediaQuery.ts`
- Modify: `web/src/grid/GridPage.tsx:156-609`
- Modify: `web/src/__tests__/grid-page.test.tsx`

- [ ] **Step 1: Add failing GridPage integration tests**

Stub `window.matchMedia` with a controllable `matches` value and change listener. Verify:

- wide mode still renders the launcher, grid, empty cells, and tile actions;
- narrow mode renders `MobileSessionView` and none of those desktop controls;
- crossing the breakpoint changes branches without a `PUT /api/layout`;
- layout plus every configured server session request must settle before empty state appears;
- a rejected server request counts as settled, contributes no sessions, and its status banner can coexist with a reachable server's selected terminal.

Hold requests with resolver functions so the “no empty flash” assertion observes each pending state.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && pnpm test src/__tests__/grid-page.test.tsx`

Expected: FAIL because GridPage always renders the desktop grid.

- [ ] **Step 3: Implement the media hook**

Create `web/src/useMediaQuery.ts`:

```ts
import { useEffect, useState } from "react";

export function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = useState(() => window.matchMedia?.(query).matches ?? false);
  useEffect(() => {
    const media = window.matchMedia?.(query);
    if (!media) return;
    const update = () => setMatches(media.matches);
    update();
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, [query]);
  return matches;
}
```

- [ ] **Step 4: Track initial settlement separately from data**

Add `layoutSettled` and `settledSessionServers` state. The first layout request sets `layoutSettled` in `.finally`. Each server's initial session request writes sessions or `[]`, then adds that server id to the settled set in `.finally`. Compute:

```ts
const initialLoading =
  !layoutSettled || servers.some((server) => !settledSessionServers.has(server.id));
```

Event-driven refreshes continue using the current refresh functions and do not reset initial settlement. A failed server must still be marked settled.

- [ ] **Step 5: Select branches without persisting**

Compute `const narrow = useMediaQuery("(max-width: 560px)")` and `mobileSessions` with `useMemo(orderMobileSessions(...))`. Keep event bridges and server banners outside the branch. Render:

```tsx
{narrow ? (
  <MobileSessionView
    sessions={mobileSessions}
    toolsByServer={toolsByServer}
    initialLoading={initialLoading}
    onRefresh={refreshSessions}
  />
) : (
  desktopBranch
)}
```

Define `desktopBranch` by moving the current `headerControls` portal and `.grid` element into one JSX fragment without changing their contents or callbacks. This is a relocation of the existing lines `322-556`, not a rewrite. Portal `headerControls` only in that wide fragment so the mobile header contains no launcher or column stepper.

- [ ] **Step 6: Run GridPage and all frontend tests**

Run: `cd web && pnpm test src/__tests__/grid-page.test.tsx src/__tests__/mobile-session-view.test.tsx`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add web/src/useMediaQuery.ts web/src/grid/GridPage.tsx web/src/__tests__/grid-page.test.tsx
git commit -m "feat(web): switch grid to mobile view at 560 pixels"
```

---

### Task 7: Public Android trust page and safe return path

**Files:**
- Create: `web/src/pages/TrustPage.tsx`
- Create: `web/src/__tests__/trust.test.tsx`
- Modify: `web/src/App.tsx:136-143`

- [ ] **Step 1: Write failing trust-page tests**

Mock `/ca/info` with all four `CAInfo` fields. Assert the page shows subject, each constrained hostname, formatted expiry, full fingerprint, `/ca.crt` download link, Pixel path, Samsung wording, LAN/VPN/tailnet warning, daemon-console comparison, mismatch stop warning, managed-device explanation, and SSH plus USB/Quick Share fallback.

Test `safeReturnTarget` directly:

```ts
expect(safeReturnTarget("/setup?code=ABC", "https://mux.local")).toBe("/setup?code=ABC");
expect(safeReturnTarget("https://mux.local/login#x", "https://mux.local")).toBe("/login#x");
expect(safeReturnTarget("//evil.test/x", "https://mux.local")).toBe("/");
expect(safeReturnTarget("https://evil.test/x", "https://mux.local")).toBe("/");
expect(safeReturnTarget("javascript:alert(1)", "https://mux.local")).toBe("/");
```

When `window.isSecureContext` is false, assert guidance and reload/check remain. When true, assert success and a Continue control using the safe destination.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && pnpm test src/__tests__/trust.test.tsx`

Expected: FAIL because `TrustPage` does not exist.

- [ ] **Step 3: Implement safe return parsing**

In `TrustPage.tsx`:

```ts
export function safeReturnTarget(raw: string | null, origin = window.location.origin): string {
  if (!raw || raw.trimStart().startsWith("//")) return "/";
  try {
    const resolved = new URL(raw, origin);
    if (resolved.origin !== origin) return "/";
    return `${resolved.pathname}${resolved.search}${resolved.hash}`;
  } catch {
    return "/";
  }
}
```

- [ ] **Step 4: Implement the trust page**

Fetch `CAInfo` from `getJSON(localServer(), "/ca/info")`. Show explicit loading and retryable error states. Use `<a href="/ca.crt" download="multimux-ca.crt">Download CA certificate</a>`. The stock path must read:

```text
Settings → Security & privacy → More security settings → Encryption & credentials →
Install a certificate → CA certificate
```

Include the Samsung equivalent, all bootstrap-security statements from the spec, and:

```tsx
{window.isSecureContext ? (
  <section className="trust-success">
    <h2>Android now trusts this multimux daemon</h2>
    <a className="primary" href={returnTarget}>Continue</a>
  </section>
) : (
  <button onClick={() => window.location.reload()}>Reload and check trust</button>
)}
```

- [ ] **Step 5: Route `/trust` before startup/auth screens**

In `App`, render `<TrustPage />` whenever `window.location.pathname === "/trust"` before setup, login, or startup-state branches. It remains reachable while setup is pending and while logged out.

- [ ] **Step 6: Run trust and startup tests**

Run: `cd web && pnpm test src/__tests__/trust.test.tsx src/__tests__/startup.test.tsx`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add web/src/pages/TrustPage.tsx web/src/__tests__/trust.test.tsx web/src/App.tsx
git commit -m "feat(web): add public Android CA trust guide"
```

---

### Task 8: Insecure-context setup and login prompts

**Files:**
- Create: `web/src/pages/TrustPrompt.tsx`
- Modify: `web/src/pages/SetupPage.tsx`
- Modify: `web/src/pages/LoginPage.tsx`
- Modify: `web/src/__tests__/login.test.tsx`
- Modify: `web/src/__tests__/startup.test.tsx`

- [ ] **Step 1: Write failing prompt tests**

Set `window.isSecureContext` to `false`. Assert setup does not render name, passkey-name, or register controls and links to:

```text
/trust?return=%2Fsetup%3Fcode%3DABC123
```

Assert login does not render “Sign in with passkey” and links to a trust URL whose decoded return includes the current path, search, and `#/connect` hash. Repeat with `isSecureContext = true` and assert existing setup/login controls remain.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && pnpm test src/__tests__/login.test.tsx src/__tests__/startup.test.tsx`

Expected: FAIL because insecure contexts still render WebAuthn controls.

- [ ] **Step 3: Implement the shared prompt**

Create:

```tsx
export function currentReturnTarget(): string {
  return `${window.location.pathname}${window.location.search}${window.location.hash}`;
}

export default function TrustPrompt() {
  const href = `/trust?return=${encodeURIComponent(currentReturnTarget())}`;
  return (
    <div className="auth-card trust-prompt">
      <p>Android must trust this daemon's local CA before passkeys and the installed app can work.</p>
      <a className="primary" href={href}>Install the Android CA</a>
    </div>
  );
}
```

- [ ] **Step 4: Gate WebAuthn controls**

In both pages, keep the wordmark but return `<TrustPrompt />` in place of the auth card only when `window.isSecureContext === false`. Using strict equality keeps non-browser test environments with an undefined property from being misclassified.

- [ ] **Step 5: Run authentication frontend tests**

Run: `cd web && pnpm test src/__tests__/login.test.tsx src/__tests__/startup.test.tsx`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/pages/TrustPrompt.tsx web/src/pages/SetupPage.tsx web/src/pages/LoginPage.tsx web/src/__tests__/login.test.tsx web/src/__tests__/startup.test.tsx
git commit -m "feat(web): gate passkeys on Android CA trust"
```

---

### Task 9: Mobile shell, viewport, safe-area, and settings CSS

**Files:**
- Modify: `web/src/App.tsx:142-169`
- Modify: `web/src/index.css`
- Modify: `web/src/__tests__/startup.test.tsx`

- [ ] **Step 1: Add failing shell assertions**

In the ready-state App test, drive the hash route and assert the app gets `grid-route` only on `#/`. Assert the Settings link always has `aria-label="Settings"` and contains a decorative icon span plus a text span so CSS can collapse it without removing its accessible name.

- [ ] **Step 2: Run the shell test to verify it fails**

Run: `cd web && pnpm test src/__tests__/startup.test.tsx`

Expected: FAIL because the route class and icon markup do not exist.

- [ ] **Step 3: Add route-aware accessible navigation markup**

Use:

```tsx
<div className={`app${route === "#/" ? " grid-route" : ""}`}>
```

Keep the wordmark linked to `#/`. Give Settings:

```tsx
<a href="#/settings" aria-label="Settings" className={route === "#/settings" ? "active" : ""}>
  <span className="settings-icon" aria-hidden="true">⚙</span>
  <span className="nav-text">Settings</span>
</a>
```

Keep Grid text unchanged outside narrow grid mode.

- [ ] **Step 4: Add responsive sizing and containment**

Append a single `@media (max-width: 560px)` block which:

- makes `.app.grid-route` exactly `100dvh`, with `100vh` as the preceding fallback;
- applies top/left/right safe-area padding to the compact app bar;
- makes `#page-root`, `.grid-page`, `.mobile-session-view`, and `.mobile-terminal` a `min-height: 0` flex chain;
- gives the terminal bottom safe-area padding;
- hides `#header-controls`, Grid navigation text/link, and `.nav-text` only in narrow grid mode while showing `.settings-icon`;
- styles `.mobile-session-header` as a one-line flex row with `touch-action: pan-y`;
- keeps `.mobile-session-position` fixed and tabular;
- gives context a larger shrink factor than title so branch/directory truncate first, then title ellipsizes;
- prevents body-level horizontal overflow;
- makes `.settings-page` narrow padding and `.tab-content` use `overflow-x: auto`, so wide tables scroll inside the active tab.

Do not change the existing wide `.grid` `calc(100vh - 60px)` rule; it belongs to the unmodified desktop branch.

- [ ] **Step 5: Run frontend lint, tests, and build**

Run: `cd web && pnpm lint && pnpm test && pnpm build`

Expected: all commands PASS. The build must report no TypeScript errors.

- [ ] **Step 6: Commit**

```bash
git add web/src/App.tsx web/src/index.css web/src/__tests__/startup.test.tsx
git commit -m "style(web): fit mobile terminal to Android viewport"
```

---

### Task 10: Android/mobile documentation and final verification

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update README navigation and quick start**

Add an Android/mobile entry to the contents. In Quick start, explain that Android users initially bypass Chrome's certificate warning only on a controlled network, then follow the daemon's `/trust` URL before registering a passkey.

- [ ] **Step 2: Add an Android subsection under Trusting the CA**

Document the exact Pixel and Samsung Settings paths, download filename, reload/check step, daemon-console fingerprint comparison, mismatch stop rule, managed-device limitation, and SSH followed by USB/Quick Share fallback. State that the certificate is public but the private `ca.key` is never exported.

- [ ] **Step 3: Document the mobile session view**

Under Using it, explain the `≤560px` portrait behavior, session ordering, header-only horizontal swipe, non-wrapping ends, single mounted terminal, lack of launch/rename/move/remove/terminate controls, and that desktop grid layout is never changed.

- [ ] **Step 4: Run focused security and UI suites**

Run:

```bash
go test ./internal/pki/ ./internal/server/ ./cmd/ -v
cd web && pnpm test src/__tests__/mobile-model.test.ts src/__tests__/mobile-session-view.test.tsx src/__tests__/grid-page.test.tsx src/__tests__/trust.test.tsx src/__tests__/login.test.tsx src/__tests__/startup.test.tsx
```

Expected: PASS.

- [ ] **Step 5: Run the repository verification**

Run: `./verify.sh`

Expected: `verify OK`, including gofmt, vet, all Go tests, frontend lint/tests/build, Go build with current web assets, and smoke test.

- [ ] **Step 6: Manually smoke-test on Android Chrome**

With a throwaway `MULTIMUX_DATA_DIR`, confirm:

1. The startup banner prints setup URL, trust URL, and the same fingerprint as `/ca/info`.
2. `/ca.crt` downloads as `multimux-ca.crt`.
3. Before CA installation, setup/login show the trust prompt.
4. After Android CA installation and reload, the trust page reports a secure context and continues to the original setup URL including its code.
5. At 560 CSS pixels, only one terminal is connected; header swipes switch sessions and do not interfere with terminal touch input.
6. Rotating wider than 560 pixels restores the existing grid without a `PUT /api/layout`.
7. Opening/closing the Android keyboard refits terminal rows without hiding content behind browser or PWA chrome.
8. Settings stays reachable and wide tables scroll inside their tab.

- [ ] **Step 7: Commit**

```bash
git add README.md
git commit -m "docs: add Android trust and mobile session guidance"
```
