# Host Browser Title Design

## Goal

Make browser tabs distinguishable by including the configured host label in the
page title.

## Behaviour

- The default title remains `multimux` while health data is unavailable or when
  no host label is configured.
- When health data contains a host label, the title is
  `multimux @<hostLabel>`.
- When the host label changes through Appearance settings, the title updates
  immediately without a page reload.

## Implementation

`App` already stores the health response and updates its `hostLabel` when it
receives the appearance event. A React effect will derive `document.title` from
that state. Keeping title generation in one effect avoids duplicating it across
the health request and appearance-event handlers.

The static title in `web/index.html` remains the loading and no-JavaScript
fallback.

## Error Handling

If the health request fails, or the response omits the optional host label, the
effect uses the existing `multimux` title.

## Testing

Frontend tests will verify that:

1. A fetched host label produces `multimux @<hostLabel>`.
2. An appearance event changes the title immediately.
3. A missing host label leaves the title as `multimux`.

The implementation will be validated with the focused frontend test and the
repository's end-to-end `./verify.sh` check.
