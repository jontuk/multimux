# Pane Text LLM Cleanup Design

## Problem

The Pane Text reader asks tmux to join rows that tmux still marks as soft
wrapped. Interactive coding agents often render prose to the current terminal
width themselves, however, so those visual line endings reach tmux as real
newlines. Selecting or copying the resulting snapshot therefore still includes
unwanted line breaks in prose.

The reader needs a semantic cleanup step that recognizes prose wrapping while
preserving intentional structure such as paragraphs, headings, lists, tables,
code blocks, shell commands, and terminal prompts.

## Decision

Automatically clean every newly captured Pane Text snapshot with an installed
coding-agent CLI:

1. Use `codex` with model `gpt-5.6-luna` when `codex` is available on the
   daemon's `PATH`.
2. Otherwise use `claude` with model `sonnet-5` when `claude` is
   available.
3. Otherwise return the untouched exact tmux snapshot with a visible warning.

Availability means that `exec.LookPath` can resolve the command in the service
environment. Codex has strict precedence: if it is installed but its invocation fails, a
Codex error does not trigger Claude. Any cleanup failure returns the complete
raw snapshot rather than a partially cleaned result.

The model does not rewrite the snapshot. It only classifies which original
newline boundaries are unwanted prose wraps. Multimux applies validated
newline-to-space edits to the original bytes. This makes it impossible for a
successful cleanup to paraphrase, omit, reorder, or invent pane content.

## User Experience

Opening Pane Text immediately shows the modal in a **Capturing and cleaning
pane text...** state. The snapshot becomes selectable only after the complete
cleanup attempt finishes, so text never changes underneath an active
selection.

On success, the existing reader displays the cleaned snapshot and **Copy all**
copies that same text. A small status identifies the processor, either Codex
(`gpt-5.6-luna`) or Claude (`sonnet-5`).

If neither CLI is installed, the selected CLI cannot authenticate, the model
is unavailable, a call times out, output is invalid, or any chunk fails, the
reader displays the untouched raw snapshot. A non-blocking warning explains
that automatic cleanup failed and that raw pane text is being shown. Copy and
selection remain available.

Refresh repeats capture and automatic cleanup. As today, the previous snapshot
remains stable and selectable while refresh is in progress. A successful
refresh atomically replaces it and scrolls to the bottom. A cleanup failure
atomically replaces it with the new raw capture and its warning. A tmux capture
or session error remains a request error: initial failure offers Retry and
Close, while refresh failure preserves the old snapshot.

Closing the reader cancels capture and every active agent process. Late results
are inert, snapshot state is discarded, and focus returns to the Text trigger.

## HTTP Boundary

Keep the existing authenticated raw endpoint unchanged:

```http
GET /api/sessions/{id}/text
```

Add an authenticated processing endpoint:

```http
POST /api/sessions/{id}/text/clean
```

POST reflects that opening the cleaned reader can consume metered model work.
It also uses the existing CSRF protection for local cookie authentication;
remote servers continue to use their bearer token.

The server resolves the database session, verifies that it is running,
captures the active tmux pane once, and passes the captured bytes to the
cleanup service. It responds with uncached JSON:

```json
{
  "text": "cleaned or untouched pane text",
  "processor": "codex",
  "model": "gpt-5.6-luna",
  "warning": ""
}
```

`processor` is `codex`, `claude`, or `raw`. `model` is empty for raw output.
`warning` is empty on successful cleanup and on snapshots that contain no
eligible boundaries. It contains short user-facing text when raw fallback was
necessary. The response always includes `Cache-Control: no-store`.

Cleanup failures are successful HTTP responses because the requested pane text
is still present and usable. Capture and session failures retain the existing
400/404/409/500 behavior. Neither raw nor cleaned pane content is logged,
persisted in the database, or written to a temporary file.

## Cleanup Engine

Add a focused server-side interface whose input is a request context and the
captured text, and whose result contains the text, processor metadata, and an
optional warning. The production implementation owns executable discovery,
chunking, agent invocation, validation, and reconstruction. The server handler
depends on the interface so tests do not invoke a real model.

### Boundary model

Split tmux output on newline characters and assign each boundary a stable
integer ID. The prompt represents lines as inert, untrusted data and asks the
model to return only the IDs whose newline should become one ASCII space.

The instructions define joins narrowly: join visually wrapped prose while
preserving paragraph gaps, headings, lists, tables, code, commands, prompts,
diagnostics, and other deliberate line structure. Blank-line boundaries are
never eligible for joining, regardless of model output.

The response conforms to this shape:

```json
{
  "join": [12, 13, 27]
}
```

The engine rejects malformed JSON, duplicate or non-integer IDs, IDs outside
the chunk's target range, and IDs that refer to ineligible boundaries. A valid
result only authorizes edits; all actual text comes from the original capture.

### Chunking

Process the entire retained snapshot in bounded chunks. Chunk size is bounded
by both bytes and line count so a single unusually long line cannot create an
unbounded request. Each chunk owns a disjoint range of newline decisions but
includes neighboring lines as read-only context. Consequently every boundary,
including a boundary at a chunk edge, is classified with text on both sides
and applied at most once.

Run only a small fixed number of chunks concurrently. Each child process has a
timeout and is tied to the HTTP request context. If the browser closes or
refresh supersedes the request, cancellation terminates all remaining child
processes. No partial result is returned: every chunk must validate before any
join is applied. An empty snapshot and a snapshot with no eligible newlines
return successfully without launching an agent.

### Codex invocation

Codex receives the exact model `gpt-5.6-luna`. Run it non-interactively from an
empty temporary working directory, with an ephemeral session, project and user
instructions ignored, repository discovery disabled, and a read-only sandbox.
The prompt and numbered pane lines go through stdin. Structured output limits
the final response to the boundary-ID object.

The temporary directory may contain a static, non-sensitive output schema but
never pane content. It is removed after the request. Stdout and stderr remain
separate; stderr may inform a generic diagnostic error but is bounded and must
never be returned directly to the browser or logged if it could echo input.

### Claude invocation

Claude receives the exact model `sonnet-5` and runs in print mode from an empty
temporary working directory. Built-in tools and slash commands are disabled and the
session is not persisted. The prompt and numbered pane lines go through stdin,
and the inline JSON schema restricts structured output to the boundary-ID
object.

As with Codex, output streams are bounded and separate, process lifetime follows
the request context, and neither prompt nor pane text is written to disk or
logs.

## Security and Privacy

Pane output is untrusted and may contain text that resembles agent
instructions. Defense does not rely only on prompting:

- the model is asked for boundary IDs rather than transformed content;
- output is schema-constrained and independently validated;
- Claude has no tools, and Codex runs away from the repository with ignored
  instructions and a read-only sandbox;
- only validated newline-to-space operations can affect the returned text;
- a failure at any point returns the byte-for-byte raw capture;
- request input and model output containing pane data are never logged;
- agent sessions are ephemeral and no pane data is stored by Multimux.

The selected coding-agent provider still receives the pane text under the
user's existing CLI account and provider configuration. The UI's processor
status makes that processing visible. This feature adds no API keys or
credentials to Multimux.

## Frontend

Add a typed API helper for the clean endpoint and update `PaneTextReader` to
store processor metadata with each snapshot. Initial loading wording reflects
both capture and cleanup. The feedback area shows successful processor status
or raw-fallback warning without changing the selectable content.

The modal's focus trap, Escape behavior, stable refresh behavior, copy action,
scroll-to-bottom behavior, and desktop/mobile integration remain unchanged.
The raw snapshot is not separately rendered when cleanup succeeds; users copy
exactly what they see.

## Testing

### Cleanup engine

Use fake `codex` and `claude` executables on a controlled `PATH`; automated
tests never make paid or network model calls. Cover:

- Codex precedence and exact `gpt-5.6-luna` selection;
- Claude fallback and exact `sonnet-5` selection;
- neither executable installed;
- Codex failure returning raw text without invoking Claude;
- exact non-interactive, isolated, non-persistent command arguments;
- prompt delivery through stdin rather than arguments or files;
- chunk size bounds, context overlap, and disjoint ownership;
- valid joins across chunk edges;
- preservation of blank lines, code, lists, tables, commands, and prompts as
  directed by the returned boundary IDs;
- malformed, duplicate, ineligible, and out-of-range IDs;
- timeout and request cancellation terminating child processes;
- one failed chunk causing atomic full-snapshot fallback;
- empty and no-eligible-boundary snapshots avoiding model calls;
- bounded stdout/stderr and errors that do not expose pane content.

### HTTP server

Use an injected cleanup double to cover authentication, CSRF, exact stored
session targeting, successful processor metadata, raw fallback metadata,
`no-store`, cancellation propagation, and all existing session/capture error
classes. The original raw endpoint remains covered and unchanged.

### Web client

Cover initial capture-and-clean loading, successful Codex and Claude status,
raw fallback warnings, Copy all using the returned text, stable refresh,
refresh fallback, stale-response rejection, cancellation, empty output, and
the existing accessibility/focus behavior. Desktop and mobile keep sharing the
same reader component.

Run the focused Go and web suites during development, then run `./verify.sh`.

## Out of Scope

- Direct terminal-selection reconstruction.
- Rewriting, summarizing, correcting, or otherwise changing pane content.
- User-selectable providers, models, prompts, or cleanup rules.
- Storing provider credentials or API keys in Multimux.
- Falling through to Claude after an installed Codex command fails.
- Persisting or caching raw or cleaned pane snapshots.
- Streaming partially cleaned text into the reader.
