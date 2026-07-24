# User-Configurable Settings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add user-configurable settings that persist in SQLite, are editable from `multimux config` and the web Settings page, starting with `confirm-terminate` (default `false`).

**Architecture:** A new `internal/config` package holds the single registry of setting names, kinds, defaults, and validation, backed by the existing `settings` key/value table in `internal/store`. The CLI (`cmd/config.go`) opens the database directly the way `multimux auth reset` does; the daemon exposes `GET`/`PUT /api/settings/preferences`; the web UI gains a Preferences panel and threads the value down to `GridPage`, which now confirms before terminating only when the setting is on.

**Tech Stack:** Go 1.x (stdlib + `modernc.org/sqlite` via `internal/store`), React 19 + TypeScript + Vite, Vitest + Testing Library for web tests.

**Spec:** `docs/superpowers/specs/2026-07-24-user-config-settings-design.md`

## Global Constraints

- Settings are stored in the existing SQLite `settings` table. **No new migration** — the table shipped in migration 0.
- One setting, three spellings: CLI `confirm-terminate`, database key `confirm_terminate`, JSON field `confirmTerminate`.
- `confirm-terminate` defaults to `"false"`. This is a deliberate behaviour change: today the browser always confirms.
- `internal/config` may import `internal/store` and nothing else in this project. `cmd` and `internal/server` import `internal/config`. Nothing imports `cmd`.
- Only `KindBool` is implemented. `Kind` exists so more kinds can be added later; do not add them now.
- No config file on disk. No live push of CLI-originated changes to open browser tabs.
- Handlers must read and write through `internal/config`, never `store.GetSetting`/`SetSetting` directly, so defaults live in one place.
- Per `CLAUDE.md`: fix every compiler, vet, lint, and test warning as it appears. `./verify.sh` runs formatting, tests, and the build end-to-end.
- Go code must be `gofmt`-clean; `verify.sh` fails on any unformatted file.

---

### Task 1: `internal/config` package

**Files:**
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: `store.Store` with `GetSetting(key string) (string, error)` and `SetSetting(key, value string) error` (both already exist in `internal/store/store.go`).
- Produces:
  - `type Kind int`, `const KindBool Kind = iota`
  - `type Key struct { Name string; Kind Kind; Default string; Help string }`
  - `var Keys []Key`
  - `func Lookup(name string) (Key, bool)`
  - `func Normalize(k Key, raw string) (string, error)`
  - `func Get(st *store.Store, name string) (string, error)`
  - `func Set(st *store.Store, name, value string) error`
  - `func Bool(st *store.Store, name string) (bool, error)`
  - `func IsDefault(st *store.Store, name string) (bool, error)`
  - `const ConfirmTerminate = "confirm-terminate"`

- [ ] **Step 1: Write the failing test**

Create `internal/config/config_test.go`:

```go
package config

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jontuk/multimux/internal/store"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "multimux.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestGetReturnsDefaultWhenUnset(t *testing.T) {
	st := testStore(t)
	got, err := Get(st, ConfirmTerminate)
	if err != nil {
		t.Fatal(err)
	}
	if got != "false" {
		t.Fatalf("Get = %q, want %q", got, "false")
	}
	b, err := Bool(st, ConfirmTerminate)
	if err != nil {
		t.Fatal(err)
	}
	if b {
		t.Fatal("Bool = true, want false (terminate must not confirm by default)")
	}
}

func TestSetGetRoundTrip(t *testing.T) {
	st := testStore(t)
	if err := Set(st, ConfirmTerminate, "true"); err != nil {
		t.Fatal(err)
	}
	got, err := Get(st, ConfirmTerminate)
	if err != nil {
		t.Fatal(err)
	}
	if got != "true" {
		t.Fatalf("Get = %q, want %q", got, "true")
	}
	b, err := Bool(st, ConfirmTerminate)
	if err != nil {
		t.Fatal(err)
	}
	if !b {
		t.Fatal("Bool = false, want true")
	}
}

func TestUnknownKeyIsRejected(t *testing.T) {
	st := testStore(t)
	if _, ok := Lookup("nope"); ok {
		t.Fatal("Lookup found an undefined key")
	}
	if _, err := Get(st, "nope"); err == nil {
		t.Fatal("Get accepted an unknown key")
	}
	// A typo must never write an orphan row.
	if err := Set(st, "nope", "true"); err == nil {
		t.Fatal("Set accepted an unknown key")
	}
	if v, _ := st.GetSetting("nope"); v != "" {
		t.Fatalf("Set wrote an orphan row: %q", v)
	}
}

func TestNormalizeBool(t *testing.T) {
	k, ok := Lookup(ConfirmTerminate)
	if !ok {
		t.Fatal("confirm-terminate is not registered")
	}
	for _, raw := range []string{"true", "false"} {
		got, err := Normalize(k, raw)
		if err != nil {
			t.Fatalf("Normalize(%q) errored: %v", raw, err)
		}
		if got != raw {
			t.Fatalf("Normalize(%q) = %q", raw, got)
		}
	}
	for _, raw := range []string{"", "yes", "1", "TRUE", "maybe"} {
		if _, err := Normalize(k, raw); err == nil {
			t.Fatalf("Normalize(%q) was accepted, want an error", raw)
		} else if !strings.Contains(err.Error(), "true") {
			t.Fatalf("error should name the accepted values, got %v", err)
		}
	}
}

func TestSetRejectsInvalidValue(t *testing.T) {
	st := testStore(t)
	if err := Set(st, ConfirmTerminate, "yes"); err == nil {
		t.Fatal("Set accepted an invalid bool")
	}
	if v, _ := st.GetSetting("confirm_terminate"); v != "" {
		t.Fatalf("invalid value was written: %q", v)
	}
}

func TestEveryKeyHasAValidDefaultAndHelp(t *testing.T) {
	for _, k := range Keys {
		if _, err := Normalize(k, k.Default); err != nil {
			t.Fatalf("key %q has an invalid default %q: %v", k.Name, k.Default, err)
		}
		if k.Help == "" {
			t.Fatalf("key %q has no help text", k.Name)
		}
		if strings.Contains(k.Name, "_") {
			t.Fatalf("key %q must use dashes, not underscores (that is the storage spelling)", k.Name)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/config/...`
Expected: FAIL — the package does not exist (`no Go files in .../internal/config` or build errors for undefined `Get`, `Set`, `Lookup`, `Normalize`, `Bool`, `Keys`, `ConfirmTerminate`).

- [ ] **Step 3: Write the implementation**

Create `internal/config/config.go`:

```go
// Package config is the single definition of every user-configurable multimux
// setting: its name, type, default, and validation. The `multimux config` CLI
// and the settings API both go through it, so they cannot disagree about what
// a setting is called, what values it accepts, or what it does when unset.
//
// Values live in the store's `settings` table under the underscored form of
// the key name (confirm-terminate -> confirm_terminate).
package config

import (
	"fmt"
	"strings"

	"github.com/jontuk/multimux/internal/store"
)

// Kind is a setting's value type. Only KindBool exists today; the type is here
// so adding a string or int setting does not mean reworking every caller.
type Kind int

const (
	KindBool Kind = iota
)

// Key describes one user-configurable setting.
type Key struct {
	Name    string // CLI name, e.g. "confirm-terminate"
	Kind    Kind
	Default string // the effective value when no row is stored
	Help    string // one line, shown by `multimux config list`
}

// ConfirmTerminate asks the browser to confirm before terminating a session.
const ConfirmTerminate = "confirm-terminate"

// Keys is every user-configurable setting, in the order `config list` prints.
var Keys = []Key{{
	Name:    ConfirmTerminate,
	Kind:    KindBool,
	Default: "false",
	Help:    "ask for confirmation before terminating a session",
}}

// Lookup finds a key by its CLI name.
func Lookup(name string) (Key, bool) {
	for _, k := range Keys {
		if k.Name == name {
			return k, true
		}
	}
	return Key{}, false
}

// storeKey is the settings-table spelling of a CLI key name.
func storeKey(name string) string { return strings.ReplaceAll(name, "-", "_") }

func unknown(name string) error {
	names := make([]string, 0, len(Keys))
	for _, k := range Keys {
		names = append(names, k.Name)
	}
	return fmt.Errorf("unknown setting %q (known settings: %s)", name, strings.Join(names, ", "))
}

// Normalize validates raw and returns the canonical stored form.
func Normalize(k Key, raw string) (string, error) {
	switch k.Kind {
	case KindBool:
		if raw != "true" && raw != "false" {
			return "", fmt.Errorf("%s takes true or false, got %q", k.Name, raw)
		}
		return raw, nil
	default:
		return "", fmt.Errorf("setting %s has an unsupported kind", k.Name)
	}
}

// Get returns the effective value: the stored row, or the key's default when
// nothing has been stored.
func Get(st *store.Store, name string) (string, error) {
	k, ok := Lookup(name)
	if !ok {
		return "", unknown(name)
	}
	v, err := st.GetSetting(storeKey(name))
	if err != nil {
		return "", err
	}
	if v == "" {
		return k.Default, nil
	}
	return v, nil
}

// Set validates value and stores it.
func Set(st *store.Store, name, value string) error {
	k, ok := Lookup(name)
	if !ok {
		return unknown(name)
	}
	v, err := Normalize(k, value)
	if err != nil {
		return err
	}
	return st.SetSetting(storeKey(name), v)
}

// Bool is Get for a KindBool setting.
func Bool(st *store.Store, name string) (bool, error) {
	v, err := Get(st, name)
	if err != nil {
		return false, err
	}
	return v == "true", nil
}

// IsDefault reports whether name has no stored row, so `config list` can say so.
func IsDefault(st *store.Store, name string) (bool, error) {
	if _, ok := Lookup(name); !ok {
		return false, unknown(name)
	}
	v, err := st.GetSetting(storeKey(name))
	if err != nil {
		return false, err
	}
	return v == "", nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/config/... -v`
Expected: PASS for all six tests.

- [ ] **Step 5: Format and vet**

Run: `gofmt -l internal/config && go vet ./internal/config/...`
Expected: no output from either.

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: add internal/config settings registry

One definition of each user-configurable setting — name, kind, default,
validation — backed by the existing settings table, so the CLI and the API
cannot disagree. First setting: confirm-terminate, default false."
```

---

### Task 2: `multimux config` CLI

**Files:**
- Create: `cmd/config.go`
- Create: `cmd/config_test.go`
- Modify: `cmd/cmd.go` (the `usage` string, the `Execute` switch, and `helpFor`)

**Interfaces:**
- Consumes: `config.Keys`, `config.Lookup`, `config.Get`, `config.Set`, `config.IsDefault` from Task 1; `dataDir()` from `cmd/ca.go:18`; `store.Open` from `internal/store`.
- Produces: `func runConfig(args []string, stdout, stderr io.Writer) int` and `const configUsage string`, both used only inside `cmd`.

- [ ] **Step 1: Write the failing test**

Create `cmd/config_test.go`:

```go
package cmd

import (
	"bytes"
	"strings"
	"testing"
	"testing/fstest"
)

// run executes the CLI against a throwaway data dir and returns code/stdout/stderr.
func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Execute(args, "dev", fstest.MapFS{}, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestConfigListShowsDefaults(t *testing.T) {
	t.Setenv("MULTIMUX_DATA_DIR", t.TempDir())
	code, out, errOut := runCLI(t, "config", "list")
	if code != 0 {
		t.Fatalf("code = %d, stderr %s", code, errOut)
	}
	if !strings.Contains(out, "confirm-terminate") {
		t.Fatalf("list missing the setting: %q", out)
	}
	if !strings.Contains(out, "false") {
		t.Fatalf("list missing the effective value: %q", out)
	}
	if !strings.Contains(out, "(default)") {
		t.Fatalf("list should mark unset keys as default: %q", out)
	}
}

func TestConfigSetThenGet(t *testing.T) {
	t.Setenv("MULTIMUX_DATA_DIR", t.TempDir())

	code, out, errOut := runCLI(t, "config", "set", "confirm-terminate", "true")
	if code != 0 {
		t.Fatalf("set code = %d, stderr %s", code, errOut)
	}
	if !strings.Contains(out, "confirm-terminate = true") {
		t.Fatalf("set should echo the new value: %q", out)
	}
	// The CLI cannot push to open tabs; say so rather than implying it can.
	if !strings.Contains(out, "reload") {
		t.Fatalf("set should say open tabs pick this up on reload: %q", out)
	}

	code, out, errOut = runCLI(t, "config", "get", "confirm-terminate")
	if code != 0 {
		t.Fatalf("get code = %d, stderr %s", code, errOut)
	}
	if strings.TrimSpace(out) != "true" {
		t.Fatalf("get should print the bare value for scripts, got %q", out)
	}

	code, out, _ = runCLI(t, "config", "list")
	if code != 0 {
		t.Fatalf("list code = %d", code)
	}
	if strings.Contains(out, "(default)") {
		t.Fatalf("a stored key must not be marked default: %q", out)
	}
}

func TestConfigRejectsUnknownKey(t *testing.T) {
	t.Setenv("MULTIMUX_DATA_DIR", t.TempDir())
	for _, args := range [][]string{
		{"config", "get", "nope"},
		{"config", "set", "nope", "true"},
	} {
		code, _, errOut := runCLI(t, args...)
		if code != 2 {
			t.Fatalf("%v code = %d, want 2", args, code)
		}
		if !strings.Contains(errOut, "nope") {
			t.Fatalf("%v error should name the key: %q", args, errOut)
		}
	}
}

func TestConfigRejectsInvalidValue(t *testing.T) {
	t.Setenv("MULTIMUX_DATA_DIR", t.TempDir())
	code, _, errOut := runCLI(t, "config", "set", "confirm-terminate", "yes")
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(errOut, "true") {
		t.Fatalf("error should name the accepted values: %q", errOut)
	}
}

func TestConfigUsageOnBadInvocation(t *testing.T) {
	t.Setenv("MULTIMUX_DATA_DIR", t.TempDir())
	for _, args := range [][]string{
		{"config"},
		{"config", "wat"},
		{"config", "get"},
		{"config", "set", "confirm-terminate"},
	} {
		code, _, errOut := runCLI(t, args...)
		if code != 2 {
			t.Fatalf("%v code = %d, want 2", args, code)
		}
		if !strings.Contains(errOut, "usage: multimux config") {
			t.Fatalf("%v should print usage, got %q", args, errOut)
		}
	}
}

func TestConfigIsDiscoverable(t *testing.T) {
	t.Setenv("MULTIMUX_DATA_DIR", t.TempDir())

	_, out, _ := runCLI(t, "help")
	if !strings.Contains(out, "config") {
		t.Fatalf("top-level usage does not mention config: %q", out)
	}

	code, out, errOut := runCLI(t, "help", "config")
	if code != 0 {
		t.Fatalf("help config code = %d, stderr %s", code, errOut)
	}
	if !strings.Contains(out, "usage: multimux config") {
		t.Fatalf("help config printed the wrong text: %q", out)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/... -run TestConfig`
Expected: FAIL — `multimux config` is an unknown command (exit code 2 with the top-level usage, so `TestConfigListShowsDefaults` fails on `code = 2`), and `helpFor` does not know `config`.

- [ ] **Step 3: Write the implementation**

Create `cmd/config.go`:

```go
package cmd

import (
	"fmt"
	"io"
	"path/filepath"
	"text/tabwriter"

	"github.com/jontuk/multimux/internal/config"
	"github.com/jontuk/multimux/internal/store"
)

const configUsage = `usage: multimux config <list|get|set> [key] [value]

Read and change user-configurable settings. Values are stored in the daemon's
database, so they survive restarts and are shared with the web Settings page.

  list             print every setting, its effective value, and whether it is
                   still at its default
  get <key>        print one setting's value, with no decoration (for scripts)
  set <key> <val>  change a setting

Examples:
  multimux config list
  multimux config get confirm-terminate
  multimux config set confirm-terminate true
`

// openStore opens the daemon's database for a CLI subcommand. The daemon may
// be running; SQLite handles the two processes.
func openStore() (*store.Store, error) {
	return store.Open(filepath.Join(dataDir(), "multimux.db"))
}

func runConfig(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, configUsage)
		return 2
	}
	switch args[0] {
	case "list":
		return configList(stdout, stderr)
	case "get":
		if len(args) != 2 {
			fmt.Fprint(stderr, configUsage)
			return 2
		}
		return configGet(args[1], stdout, stderr)
	case "set":
		if len(args) != 3 {
			fmt.Fprint(stderr, configUsage)
			return 2
		}
		return configSet(args[1], args[2], stdout, stderr)
	default:
		fmt.Fprint(stderr, configUsage)
		return 2
	}
}

func configList(stdout, stderr io.Writer) int {
	st, err := openStore()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer st.Close()

	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, k := range config.Keys {
		v, err := config.Get(st, k.Name)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		isDefault, err := config.IsDefault(st, k.Name)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		suffix := ""
		if isDefault {
			suffix = "(default)"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", k.Name, v, suffix, k.Help)
	}
	tw.Flush()
	return 0
}

func configGet(name string, stdout, stderr io.Writer) int {
	if _, ok := config.Lookup(name); !ok {
		fmt.Fprintf(stderr, "unknown setting %q — run \"multimux config list\" to see them all\n", name)
		return 2
	}
	st, err := openStore()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer st.Close()

	v, err := config.Get(st, name)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, v)
	return 0
}

func configSet(name, value string, stdout, stderr io.Writer) int {
	k, ok := config.Lookup(name)
	if !ok {
		fmt.Fprintf(stderr, "unknown setting %q — run \"multimux config list\" to see them all\n", name)
		return 2
	}
	// Validate before opening the database so a typo cannot create one.
	if _, err := config.Normalize(k, value); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	st, err := openStore()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer st.Close()

	if err := config.Set(st, name, value); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "%s = %s\n", name, value)
	// This process is not the daemon, so it cannot push the change to open
	// tabs; say what actually happens instead of implying it is instant.
	fmt.Fprintln(stdout, "open browser tabs pick this up on reload.")
	return 0
}
```

- [ ] **Step 4: Wire it into the CLI**

In `cmd/cmd.go`, add to the `commands:` block of `usage`, immediately after the `ca trust` entry and before `auth reset`:

```
  config list|get|set            read and change user-configurable settings
```

Add to the `Examples:` block of `usage`, after the existing `multimux ca trust --remote` line:

```
  multimux config set confirm-terminate true   confirm before terminating a session
```

In `Execute`'s switch, add a case after `case "auth":`:

```go
	case "config":
		return runConfig(args[1:], stdout, stderr)
```

In `helpFor`'s switch, add a case after `case "auth":`:

```go
	case "config":
		fmt.Fprint(stdout, configUsage)
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./cmd/... -run TestConfig -v`
Expected: PASS for all six tests.

Then run the whole package so the wiring did not break existing CLI tests:

Run: `go test ./cmd/...`
Expected: PASS.

- [ ] **Step 6: Try it by hand**

Run:
```bash
MULTIMUX_DATA_DIR=$(mktemp -d) go run . config list
```
Expected: one aligned row — `confirm-terminate  false  (default)  ask for confirmation before terminating a session`.

- [ ] **Step 7: Format and vet**

Run: `gofmt -l cmd && go vet ./cmd/...`
Expected: no output from either.

- [ ] **Step 8: Commit**

```bash
git add cmd/config.go cmd/config_test.go cmd/cmd.go
git commit -m "feat: add multimux config list/get/set

Reads and writes the settings registry from the shell, opening the daemon
database directly the way auth reset does. get prints a bare value so it
scripts cleanly; set says open tabs pick the change up on reload, because
the CLI is a separate process and cannot push to them."
```

---

### Task 3: preferences HTTP API

**Files:**
- Modify: `internal/server/api.go` (add two handlers after `handlePutAppearance`)
- Modify: `internal/server/server.go:81` (register two routes after the appearance routes)
- Test: `internal/server/api_test.go` (append)

**Interfaces:**
- Consumes: `config.Bool`, `config.Set`, `config.ConfirmTerminate` from Task 1; `s.cfg.Store`, `writeJSON`, `readJSON`, and the test helpers `newTestServer`/`do` already in `internal/server`.
- Produces: `GET /api/settings/preferences` → `{"confirmTerminate": bool}`; `PUT /api/settings/preferences` ← `{"confirmTerminate": bool}` → `{"confirmTerminate": bool}`.

- [ ] **Step 1: Write the failing test**

Append to `internal/server/api_test.go`:

```go
func TestPreferencesDefaultAndRoundTrip(t *testing.T) {
	s, _, am := newTestServer(t, true)
	token, _ := am.CreateSession("UA")

	var resp map[string]any
	w := do(t, s, "GET", "/api/settings/preferences", token)
	if w.Code != 200 {
		t.Fatalf("get preferences = %d: %s", w.Code, w.Body.String())
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	// Default is off: terminating must not confirm unless asked.
	if resp["confirmTerminate"] != false {
		t.Fatalf("confirmTerminate = %v, want false", resp["confirmTerminate"])
	}

	w = do(t, s, "PUT", "/api/settings/preferences", token, `{"confirmTerminate":true}`)
	if w.Code != 200 {
		t.Fatalf("put preferences = %d: %s", w.Code, w.Body.String())
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["confirmTerminate"] != true {
		t.Fatalf("put should echo the stored state, got %v", resp)
	}

	w = do(t, s, "GET", "/api/settings/preferences", token)
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["confirmTerminate"] != true {
		t.Fatalf("confirmTerminate did not persist: %v", resp)
	}
}

func TestPreferencesRejectsBadBody(t *testing.T) {
	s, _, am := newTestServer(t, true)
	token, _ := am.CreateSession("UA")

	if w := do(t, s, "PUT", "/api/settings/preferences", token, `{`); w.Code != 400 {
		t.Fatalf("malformed body = %d, want 400", w.Code)
	}
	if w := do(t, s, "PUT", "/api/settings/preferences", token, `{"confirmTerminate":"yes"}`); w.Code != 400 {
		t.Fatalf("wrong type = %d, want 400", w.Code)
	}
}

func TestPreferencesRequireAuth(t *testing.T) {
	s, _, _ := newTestServer(t, true)

	if w := do(t, s, "GET", "/api/settings/preferences", ""); w.Code != 401 {
		t.Fatalf("unauthenticated GET = %d, want 401", w.Code)
	}
	if w := do(t, s, "PUT", "/api/settings/preferences", "", `{"confirmTerminate":true}`); w.Code != 401 {
		t.Fatalf("unauthenticated PUT = %d, want 401", w.Code)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/server/... -run TestPreferences -v`
Expected: FAIL — the route is unregistered, so `GET` returns 404 rather than 200.

- [ ] **Step 3: Write the handlers**

In `internal/server/api.go`, add after `handlePutAppearance`:

```go
func (s *Server) handleGetPreferences(w http.ResponseWriter, r *http.Request) {
	confirmTerminate, err := config.Bool(s.cfg.Store, config.ConfirmTerminate)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"confirmTerminate": confirmTerminate})
}

func (s *Server) handlePutPreferences(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ConfirmTerminate bool `json:"confirmTerminate"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad body"})
		return
	}
	if err := config.Set(s.cfg.Store, config.ConfirmTerminate, strconv.FormatBool(in.ConfirmTerminate)); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	slog.Info("preferences changed", "keys", []string{config.ConfirmTerminate})
	// Echo the stored state so the client reconciles against the daemon.
	writeJSON(w, 200, map[string]any{"confirmTerminate": in.ConfirmTerminate})
}
```

Add `"strconv"` and `"github.com/jontuk/multimux/internal/config"` to the import block of `internal/server/api.go` if they are not already there.

- [ ] **Step 4: Register the routes**

In `internal/server/server.go`, immediately after the line registering `PUT /api/settings/appearance`:

```go
	s.mux.HandleFunc("GET /api/settings/preferences", s.handleGetPreferences)
	s.mux.HandleFunc("PUT /api/settings/preferences", s.handlePutPreferences)
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/server/... -run TestPreferences -v`
Expected: PASS for all three tests.

If `TestPreferencesRejectsBadBody` fails on the `"yes"` case, `readJSON` is not erroring on a type mismatch — check whether it uses `DisallowUnknownFields`/`Decode`; a `json.Decode` into a `bool` field does reject a string, so a failure here means the handler is reading the body some other way. Fix the handler, not the test.

- [ ] **Step 6: Format and vet**

Run: `gofmt -l internal/server && go vet ./internal/server/...`
Expected: no output from either.

- [ ] **Step 7: Commit**

```bash
git add internal/server/api.go internal/server/server.go internal/server/api_test.go
git commit -m "feat: serve preferences over the settings API

GET/PUT /api/settings/preferences read and write through internal/config,
so the browser and the CLI share one definition of the default."
```

---

### Task 4: web Preferences panel

**Files:**
- Create: `web/src/settings/PreferencesPanel.tsx`
- Create: `web/src/__tests__/preferences.test.tsx`
- Modify: `web/src/pages/SettingsPage.tsx`

**Interfaces:**
- Consumes: `GET`/`PUT /api/settings/preferences` from Task 3; `useFetch`, `PanelState`, `putJSON`, `errorText`, `localServer` already in the web app.
- Produces:
  - default export `PreferencesPanel`
  - `export const PREFERENCES_EVENT = "multimux:preferences"`
  - `export type PreferencesDetail = { confirmTerminate: boolean }`
  - `export type Preferences = { confirmTerminate: boolean }`

- [ ] **Step 1: Write the failing test**

Create `web/src/__tests__/preferences.test.tsx`:

```tsx
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, vi } from "vitest";
import PreferencesPanel, { PREFERENCES_EVENT } from "../settings/PreferencesPanel";

afterEach(() => {
  vi.restoreAllMocks();
});

test("preferences panel loads, saves, and dispatches the update event", async () => {
  const fetchMock = vi
    .spyOn(globalThis, "fetch")
    .mockResolvedValueOnce(new Response(JSON.stringify({ confirmTerminate: false })))
    .mockResolvedValueOnce(new Response(JSON.stringify({ confirmTerminate: true })))
    .mockResolvedValueOnce(new Response(JSON.stringify({ confirmTerminate: true })));

  const events: CustomEvent[] = [];
  const listener = (e: Event) => events.push(e as CustomEvent);
  window.addEventListener(PREFERENCES_EVENT, listener);

  render(<PreferencesPanel />);
  const box = (await screen.findByLabelText(/ask before terminating/i)) as HTMLInputElement;
  expect(box.checked).toBe(false);

  await userEvent.click(box);
  await userEvent.click(screen.getByText("Save"));

  await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3));
  const put = fetchMock.mock.calls[1];
  expect(String(put[0])).toContain("/api/settings/preferences");
  expect((put[1] as RequestInit).method).toBe("PUT");
  expect(JSON.parse((put[1] as RequestInit).body as string)).toEqual({ confirmTerminate: true });
  expect(events).toHaveLength(1);
  expect(events[0].detail).toEqual({ confirmTerminate: true });

  window.removeEventListener(PREFERENCES_EVENT, listener);
});

test("preferences panel surfaces a load failure with a retry", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("nope", { status: 500 }));

  render(<PreferencesPanel />);
  await screen.findByText("Retry");
  expect(screen.queryByLabelText(/ask before terminating/i)).not.toBeInTheDocument();
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && pnpm test -- preferences`
Expected: FAIL — `Cannot find module '../settings/PreferencesPanel'`.

- [ ] **Step 3: Write the panel**

Create `web/src/settings/PreferencesPanel.tsx`:

```tsx
import { useCallback, useState } from "react";
import { errorText, putJSON } from "../api";
import { localServer } from "../servers";
import { useFetch } from "../useFetch";
import PanelState from "./PanelState";

export type Preferences = { confirmTerminate: boolean };

/** Fired after a save so the grid honours the new setting without a reload. */
export const PREFERENCES_EVENT = "multimux:preferences";
export type PreferencesDetail = Preferences;

export default function PreferencesPanel() {
  const [confirmTerminate, setConfirmTerminate] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState("");

  const seed = useCallback((p: Preferences) => {
    setConfirmTerminate(p.confirmTerminate);
  }, []);
  const { data: prefs, error, loading, reload } = useFetch<Preferences>("/api/settings/preferences", seed);

  async function save() {
    if (!prefs) return;
    setSaveError("");
    try {
      setSaving(true);
      await putJSON(localServer(), "/api/settings/preferences", { confirmTerminate });
      window.dispatchEvent(new CustomEvent<PreferencesDetail>(PREFERENCES_EVENT, { detail: { confirmTerminate } }));
      reload();
    } catch (err) {
      setSaveError(errorText(err));
    } finally {
      setSaving(false);
    }
  }

  return (
    <section>
      <h2>Preferences</h2>
      <p className="settings-hint">
        Behaviour of this daemon's web UI. Also settable from the shell with <code>multimux config</code>.
      </p>
      <PanelState loading={loading} error={error} onRetry={reload} />
      {saveError && <div className="server-status-banner">{saveError}</div>}
      {prefs && !loading && !error && (
        <>
          <div className="settings-fields">
            <label>
              <input
                type="checkbox"
                checked={confirmTerminate}
                onChange={(e) => setConfirmTerminate(e.target.checked)}
                disabled={saving}
              />
              Ask before terminating a session
            </label>
          </div>
          <button className="primary" disabled={saving} onClick={save}>
            Save
          </button>
        </>
      )}
    </section>
  );
}
```

- [ ] **Step 4: Add the tab**

In `web/src/pages/SettingsPage.tsx`:

- Add the import after the `AppearancePanel` import:

```tsx
import PreferencesPanel from "../settings/PreferencesPanel";
```

- Extend the `Tab` union with `| "preferences"`:

```tsx
type Tab = "tools" | "dirs" | "passkeys" | "sessions" | "servers" | "daemon" | "appearance" | "preferences";
```

- Add the entry at the end of the `tabs` array, after the `appearance` entry:

```tsx
    { id: "preferences", label: "Preferences", component: <PreferencesPanel /> },
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd web && pnpm test -- preferences`
Expected: PASS for both tests.

- [ ] **Step 6: Lint and typecheck**

Run: `cd web && pnpm lint && pnpm build`
Expected: no errors from either.

- [ ] **Step 7: Commit**

```bash
git add web/src/settings/PreferencesPanel.tsx web/src/__tests__/preferences.test.tsx web/src/pages/SettingsPage.tsx
git commit -m "feat: add a Preferences settings panel

Toggles confirm-terminate from the browser and dispatches an event so an
open grid honours the change without a reload."
```

---

### Task 5: honour the setting in the grid

**Files:**
- Modify: `web/src/grid/GridPage.tsx:62` (props) and `:189-191` (the terminate guard)
- Modify: `web/src/App.tsx` (fetch preferences, listen for the event, pass the prop)
- Test: `web/src/__tests__/grid-page.test.tsx:206` (update) plus a new case

**Interfaces:**
- Consumes: `PREFERENCES_EVENT`, `PreferencesDetail`, `Preferences` from Task 4; `GET /api/settings/preferences` from Task 3; `getJSON`/`localServer` already used in `App.tsx`.
- Produces: `GridPage` accepts an optional `confirmTerminate?: boolean` prop, defaulting to `false`.

- [ ] **Step 1: Write the failing tests**

In `web/src/__tests__/grid-page.test.tsx`, replace the existing test at line 206 ("terminate button confirms then DELETEs the session and drops the tile") with these two:

```tsx
test("terminate skips the confirm prompt by default", async () => {
  const layout = { shape: { rows: 1, cols: 2 }, tiles: [{ serverId: "local", sessionId: 1 }, null] };
  const fetchMock = mockFetch(layout);
  const confirmMock = vi.spyOn(window, "confirm").mockReturnValue(true);

  render(<GridPage />);
  await screen.findByTestId("term-1");

  await userEvent.click(screen.getByLabelText("terminate session 1"));
  await waitFor(() => expect(screen.queryByTestId("term-1")).not.toBeInTheDocument());
  expect(confirmMock).not.toHaveBeenCalled();
  const delCall = fetchMock.mock.calls.find(([, init]) => init?.method === "DELETE");
  expect(String(delCall?.[0])).toContain("/api/sessions/1");
});

test("terminate confirms first when confirmTerminate is on", async () => {
  const layout = { shape: { rows: 1, cols: 2 }, tiles: [{ serverId: "local", sessionId: 1 }, null] };
  const fetchMock = mockFetch(layout);
  const confirmMock = vi.spyOn(window, "confirm").mockReturnValue(false);

  render(<GridPage confirmTerminate />);
  await screen.findByTestId("term-1");

  // Declining leaves the session alone.
  await userEvent.click(screen.getByLabelText("terminate session 1"));
  expect(confirmMock).toHaveBeenCalledTimes(1);
  expect(screen.getByTestId("term-1")).toBeInTheDocument();
  expect(fetchMock.mock.calls.some(([, init]) => init?.method === "DELETE")).toBe(false);

  // Accepting goes through.
  confirmMock.mockReturnValue(true);
  await userEvent.click(screen.getByLabelText("terminate session 1"));
  await waitFor(() => expect(screen.queryByTestId("term-1")).not.toBeInTheDocument());
  const delCall = fetchMock.mock.calls.find(([, init]) => init?.method === "DELETE");
  expect(String(delCall?.[0])).toContain("/api/sessions/1");
});
```

Append to `web/src/__tests__/preferences.test.tsx`:

```tsx
test("app fetches preferences at startup and follows the update event", async () => {
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation((input) => {
    const url = String(input);
    if (url.includes("/api/settings/preferences")) {
      return Promise.resolve(new Response(JSON.stringify({ confirmTerminate: true })));
    }
    if (url.includes("/healthz")) {
      return Promise.resolve(new Response(JSON.stringify({ status: "ok", setupPending: false, version: "test" })));
    }
    if (url.includes("/api/auth/me")) return Promise.resolve(new Response("{}", { status: 200 }));
    return Promise.resolve(new Response("[]"));
  });

  const { default: App } = await import("../App");
  render(<App />);

  await waitFor(() =>
    expect(fetchMock.mock.calls.some(([u]) => String(u).includes("/api/settings/preferences"))).toBe(true),
  );
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && pnpm test -- grid-page preferences`
Expected: FAIL — `terminate skips the confirm prompt by default` fails because `window.confirm` is still called unconditionally, and `<GridPage confirmTerminate />` is a TypeScript error (unknown prop).

- [ ] **Step 3: Add the prop to GridPage**

In `web/src/grid/GridPage.tsx`, change the component signature at line 62 from:

```tsx
export default function GridPage({ headerSlot = null }: { headerSlot?: HTMLElement | null }) {
```

to:

```tsx
export default function GridPage({
  headerSlot = null,
  confirmTerminate = false,
}: {
  headerSlot?: HTMLElement | null;
  // Off by default: terminating is one click unless the user opts in.
  confirmTerminate?: boolean;
}) {
```

Change the guard in `terminateSession` from:

```tsx
    if (!window.confirm(`Terminate session #${sessionId}?`)) return;
```

to:

```tsx
    if (confirmTerminate && !window.confirm(`Terminate session #${sessionId}?`)) return;
```

- [ ] **Step 4: Wire App.tsx**

In `web/src/App.tsx`:

- Add the import after the `APPEARANCE_EVENT` import:

```tsx
import { PREFERENCES_EVENT, type Preferences, type PreferencesDetail } from "./settings/PreferencesPanel";
```

- Add state beside the other `useState` calls in `App`:

```tsx
  const [confirmTerminate, setConfirmTerminate] = useState(false);
```

- Add two effects after the existing `APPEARANCE_EVENT` effect:

```tsx
  // Preferences are read once at startup; a failure leaves the defaults in
  // place rather than blocking the app on a non-essential fetch.
  useEffect(() => {
    getJSON<Preferences>(localServer(), "/api/settings/preferences")
      .then((p) => setConfirmTerminate(p.confirmTerminate))
      .catch(() => {});
  }, []);

  useEffect(() => {
    const onPreferences = (e: Event) => {
      setConfirmTerminate((e as CustomEvent<PreferencesDetail>).detail.confirmTerminate);
    };
    window.addEventListener(PREFERENCES_EVENT, onPreferences);
    return () => window.removeEventListener(PREFERENCES_EVENT, onPreferences);
  }, []);
```

- Change the grid route render (line 135) from:

```tsx
        {route === "#/" && <GridPage headerSlot={headerSlot} />}
```

to:

```tsx
        {route === "#/" && <GridPage headerSlot={headerSlot} confirmTerminate={confirmTerminate} />}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd web && pnpm test -- grid-page preferences`
Expected: PASS.

Then the whole web suite, since `App` now makes an extra fetch that other tests' mocks must tolerate:

Run: `cd web && pnpm test`
Expected: PASS. If a test fails with `unmocked fetch: .../api/settings/preferences`, add `"/api/settings/preferences": () => new Response(JSON.stringify({ confirmTerminate: false }))` to that test's route map — the app is correct, the mock is incomplete.

- [ ] **Step 6: Lint and typecheck**

Run: `cd web && pnpm lint && pnpm build`
Expected: no errors from either.

- [ ] **Step 7: Commit**

```bash
git add web/src/grid/GridPage.tsx web/src/App.tsx web/src/__tests__/grid-page.test.tsx web/src/__tests__/preferences.test.tsx
git commit -m "feat: confirm before terminating only when configured

GridPage takes confirmTerminate, off by default, so terminating is one
click unless the user opts in. App reads it at startup and follows the
Preferences panel's event."
```

---

### Task 6: documentation and end-to-end verification

**Files:**
- Modify: `README.md`

**Interfaces:**
- Consumes: everything from Tasks 1–5. Produces nothing consumed by later tasks.

- [ ] **Step 1: Document the command**

`README.md` has a `## Commands` section at line 119 whose fenced block lists every subcommand. Add a line to that block, after the `multimux ca trust` line:

~~~
multimux config list|get|set                 read and change user-configurable settings
~~~

Then add a new `### Settings` subsection immediately after that section's closing prose — after the paragraph ending `(\`tmux -L multimux ls\` lists them).` and before `## Security model`:

~~~markdown
### Settings

Some behaviour is configurable, from the shell or from the web Settings page's
**Preferences** tab. Both write to the same daemon database.

```sh
multimux config list                          # every setting and its value
multimux config get confirm-terminate         # one value, bare, for scripts
multimux config set confirm-terminate true    # change it
```

| Setting | Default | Effect |
| --- | --- | --- |
| `confirm-terminate` | `false` | Ask for confirmation before terminating a session. |

Changes made from the shell reach open browser tabs on their next reload;
changes made from the Preferences tab apply immediately.
~~~

- [ ] **Step 2: Run the full verification**

Run: `./verify.sh`
Expected: the script prints `gofmt`, `go vet`, `go test`, `web`, `go build`, `smoke`, and finally `verify OK`. Fix anything it reports — per `CLAUDE.md`, no warnings or errors may be left behind.

- [ ] **Step 3: Exercise the real binary**

Run:
```bash
export MULTIMUX_DATA_DIR=$(mktemp -d)
go run . config list
go run . config set confirm-terminate true
go run . config get confirm-terminate
go run . help config
go run . config set confirm-terminate maybe; echo "exit=$?"
```
Expected: `list` shows `false (default)`; `set` echoes `confirm-terminate = true` plus the reload note; `get` prints `true`; `help config` prints the usage; the bad value prints an error naming `true`/`false` and `exit=2`.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: document multimux config and confirm-terminate"
```

- [ ] **Step 5: Review the branch**

Run: `git log --oneline main..HEAD` and `git diff main...HEAD --stat`
Expected: seven commits (spec, config package, CLI, API, panel, grid wiring, docs) touching `internal/config/`, `cmd/`, `internal/server/`, `web/src/`, `README.md`, and `docs/superpowers/`.

---

## Notes for the implementer

- `dataDir()` lives in `cmd/ca.go:18` and honours `MULTIMUX_DATA_DIR`, which is what makes the CLI tests hermetic. Always set it with `t.Setenv` in a test.
- `store.Open` creates the database and runs migrations, so a CLI subcommand against a fresh data directory works without a daemon ever having run.
- The web tests mock `fetch` per test. Adding a startup fetch to `App` is the one change in this plan that can break unrelated tests; Task 5 Step 5 says what to do.
- Existing panels put their save button outside the fields `div` and use `className="primary"` — `PreferencesPanel` follows that so it picks up the existing styles without new CSS.
