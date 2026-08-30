# Mobile Session Creation Design

## Goal

Allow a phone user to create sessions without switching to a wider viewport.
The mobile creator exposes the desktop launcher's complete choice set: server,
tool, directory, and optional subdirectory.

The creator is available only in the narrow grid view at the existing
`max-width: 560px` breakpoint. The desktop launcher and Settings layouts remain
visually unchanged.

## Mobile entry point

The consolidated mobile header gains a compact `+` button with the accessible
name **New session**. Its left-to-right control order is:

1. New session (`+`)
2. Fit
3. Compose
4. the existing numeric font-size selector
5. the existing Settings gear

The visible passive-size action changes from **Fit session to phone** to
**Fit**. Its accessible name continues to explain that it fits the session to
the phone, and its existing confirmation continues to warn that other attached
clients will reflow.

The New session button and Settings remain available while sessions are
loading and when no sessions are running. Session-dependent Fit, Compose, and
font controls remain absent when there is no selected terminal. The empty state
no longer says that launching requires a wider device.

## Full-screen creator

Activating `+` replaces the visible mobile session chrome with a mobile-only,
full-screen creator. The creator uses the measured visible viewport height and
safe-area insets already used by the mobile route. It contains:

- a back/close action and **New session** title;
- a server selector when more than one server is configured;
- a tool selector;
- a directory selector;
- an optional subdirectory field;
- the subdirectory history and filesystem suggestions available on desktop;
- an inline loading, configuration, or failure message when needed; and
- a full-width **Create session** action.

The subdirectory field retains desktop behavior: history is substring-filtered,
filesystem children complete the current path segment, remembered entries can
be forgotten, Enter either accepts a highlighted suggestion or submits, and
Escape first closes an open suggestion list. Escape with no suggestion list
open closes the creator. The visible back action also closes it. Closing never
sends a launch request.

The ordinary mobile header and selected terminal remain mounted but hidden
while the creator is visible. Hidden content is unavailable to pointer,
keyboard, and accessibility navigation. This preserves the PTY connection and
any Compose draft. Restoring the terminal relies on its existing observation
and fit behavior to match the visible box again.

## Defaults

When a session is selected, opening the creator targets that session's server
and working path. The path is split against that server's configured launch
directories using the same longest-prefix behavior as the desktop launcher;
the configured root becomes the directory selection and the remainder becomes
the subdirectory value.

When there is no selected session, the creator starts on the first configured
server and uses that server's first tool and first directory after they load.
Changing servers or directories resets dependent selections and stale
suggestions exactly as it does on desktop.

Each time the creator is opened it starts from these current defaults. A
cancelled or failed attempt does not become the default for a later opening.

## Shared launcher behavior

Extract the stateful launch behavior from `HeaderLauncher` into a shared model,
implemented as a focused hook or equivalent controller. It owns:

- current server, tool, directory, and subdirectory selections;
- loading tools and directories for the selected server;
- applying a target server and working path;
- subdirectory history and child-directory requests;
- suggestion filtering, highlighting, and forgetting;
- stale-response protection across server and directory changes;
- launch eligibility, in-flight state, and errors; and
- the `POST /api/sessions` request and ordered batch result.

The desktop `HeaderLauncher` keeps its compact presentation and delegates to
the shared behavior. It consumes a successful batch by placing every returned
session in the saved grid, preserving current desktop behavior.

A separate mobile creator presents the same model as a vertically stacked
form. Shared behavior, rather than duplicated effects and request code, keeps
the two presentations consistent while allowing each viewport to use suitable
markup and styling.

## Successful creation and selection

The session endpoint may return multiple sessions when a tool command contains
the group separator. The shared model treats this as one ordered result.

On mobile success:

1. Record the key of the first returned session as the pending mobile
   selection.
2. Close the creator.
3. Refresh session data.
4. When the refreshed mobile list contains that key, select it.

The prior terminal may remain visible briefly while the authoritative refresh
is in flight. If the endpoint returns an empty list, close the creator and
refresh without changing selection.

Mobile creation does not add any returned session to the desktop grid and does
not write `/api/layout`. The sessions remain unplaced, while still appearing in
the mobile list after its placed-session prefix. If several sessions were
created, only the first is initially selected; the rest are reachable through
normal mobile session navigation.

Selection remains ephemeral and browser-local, as it is today.

## Loading, configuration, and errors

Opening the creator starts or reuses only the requests needed for its selected
server. While tools and directories are loading, the Create session action is
disabled. Switching servers immediately clears the previous server's
per-daemon identifiers and disables submission until the new choices arrive.

If the selected server cannot be reached, the creator identifies that server
and remains open so the user can retry or select another server. If tools or
directories are not configured, it links to Settings using the same guidance
as the desktop launcher.

A failed launch leaves all fields editable, keeps the creator open, and shows
the launch error inline. The Create session action is disabled only while a
request is in flight, so the user can retry. A successful launch updates the
used subdirectory's local history only if the server and directory selection
still match the request that completed, preserving the desktop stale-result
guard.

## Accessibility and responsive behavior

Every selector and action has a programmatic label. The compact `+` and Fit
labels use longer accessible names than their visible text. Focus moves into
the creator when it opens and returns to the New session button when it closes.
The hidden terminal view cannot retain active focus while the creator is open.

The creator exists only in the narrow rendering branch. Crossing to the wide
branch unmounts it and shows the existing desktop header launcher. Crossing the
breakpoint never launches a session by itself and does not write layout state.

## Testing

Frontend tests cover:

- the extracted model preserving server changes, target-path selection,
  history, child suggestions, stale-result guards, forgetting, launch errors,
  retries, and grouped results;
- existing desktop launcher defaults, rendering, and placement behavior after
  the extraction;
- the exact mobile control order: New session, Fit, Compose, font selector,
  Settings;
- Fit's short visible label, descriptive accessible name, connection-state
  disabling, confirmation, and one active resize;
- New session availability during loading, with a selected session, and in the
  empty state;
- selected-session server/path defaults and first-server/tool/directory
  defaults when no session exists;
- the creator's full-screen presentation, labels, safe-area treatment,
  keyboard behavior, closing, and focus restoration;
- the background terminal remaining mounted but unavailable while the creator
  is open;
- unconfigured and unreachable-server states;
- failed launches remaining open and editable;
- successful launches closing, refreshing, leaving `/api/layout` untouched,
  and selecting the first returned session after refresh;
- additional grouped sessions remaining navigable and unplaced; and
- crossing the mobile breakpoint without launching or persisting layout.

Run `./verify.sh` after implementation to exercise formatting, linting, all Go
and frontend tests, both builds, and the smoke check.
