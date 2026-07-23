import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";
import ToolsPanel from "../settings/ToolsPanel";
import PasskeysPanel from "../settings/PasskeysPanel";
import AuthSessionsPanel from "../settings/AuthSessionsPanel";
import DaemonPanel from "../settings/DaemonPanel";

test("lists and adds tools", async () => {
  const fetchMock = vi
    .spyOn(globalThis, "fetch")
    .mockResolvedValueOnce(new Response(JSON.stringify([{ id: 1, name: "zsh", command: "zsh" }])))
    .mockResolvedValueOnce(new Response(JSON.stringify({ id: 2, name: "claude", command: "claude" }), { status: 201 }))
    .mockResolvedValueOnce(
      new Response(
        JSON.stringify([
          { id: 1, name: "zsh", command: "zsh" },
          { id: 2, name: "claude", command: "claude" },
        ]),
      ),
    );

  render(<ToolsPanel />);
  await screen.findByText((content, element) => content === "zsh" && element?.tagName === "TD");

  await userEvent.type(screen.getByPlaceholderText("name"), "claude");
  await userEvent.type(screen.getByPlaceholderText("command"), "claude");
  await userEvent.click(screen.getByText("Add tool"));

  await waitFor(() =>
    expect(
      screen.getByText((content, element) => content === "claude" && element?.tagName === "TD"),
    ).toBeInTheDocument(),
  );
  expect(fetchMock).toHaveBeenCalledTimes(3);
  fetchMock.mockRestore();
});

test("edits a tool via PUT /api/tools/{id}", async () => {
  const fetchMock = vi
    .spyOn(globalThis, "fetch")
    // initial list
    .mockResolvedValueOnce(new Response(JSON.stringify([{ id: 1, name: "zsh", command: "zsh" }])))
    // PUT update
    .mockResolvedValueOnce(new Response(JSON.stringify({ id: 1, name: "bash", command: "bash -l" }), { status: 200 }))
    // refresh list
    .mockResolvedValueOnce(new Response(JSON.stringify([{ id: 1, name: "bash", command: "bash -l" }])));

  render(<ToolsPanel />);
  await screen.findByText((content, element) => content === "zsh" && element?.tagName === "TD");

  await userEvent.click(screen.getByText("edit"));
  const nameInput = screen.getByLabelText("edit name");
  const commandInput = screen.getByLabelText("edit command");
  await userEvent.clear(nameInput);
  await userEvent.type(nameInput, "bash");
  await userEvent.clear(commandInput);
  await userEvent.type(commandInput, "bash -l");
  await userEvent.click(screen.getByText("save"));

  await waitFor(() =>
    expect(screen.getByText((content, element) => content === "bash" && element?.tagName === "TD")).toBeInTheDocument(),
  );

  const putCall = fetchMock.mock.calls.find(([, init]) => (init as RequestInit)?.method === "PUT");
  expect(putCall).toBeDefined();
  const [url, init] = putCall!;
  expect(String(url)).toContain("/api/tools/1");
  expect(JSON.parse((init as RequestInit).body as string)).toEqual({ name: "bash", command: "bash -l" });
  fetchMock.mockRestore();
});

test("passkeys render backend createdAt/lastUsedAt shape", async () => {
  const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValueOnce(
    new Response(
      JSON.stringify([
        { id: "a", name: "yubikey", createdAt: "2026-01-02T00:00:00Z", lastUsedAt: "2026-03-04T00:00:00Z" },
        { id: "b", name: "phone", createdAt: "2026-01-05T00:00:00Z", lastUsedAt: null },
      ]),
    ),
  );

  render(<PasskeysPanel />);
  const yubikeyRow = (await screen.findByText("yubikey")).closest("tr")!;
  expect(within(yubikeyRow).queryByText("Invalid Date")).toBeNull();

  const phoneRow = screen.getByText("phone").closest("tr")!;
  expect(within(phoneRow).getByText("Never")).toBeInTheDocument();
  fetchMock.mockRestore();
});

test("auth sessions render backend createdAt/expiresAt shape", async () => {
  const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValueOnce(
    new Response(
      JSON.stringify([
        {
          tokenHash: "abc123",
          userAgent: "Mozilla",
          createdAt: "2026-01-02T00:00:00Z",
          expiresAt: "2026-02-02T00:00:00Z",
        },
      ]),
    ),
  );

  render(<AuthSessionsPanel />);
  const row = (await screen.findByText("Mozilla")).closest("tr")!;
  expect(within(row).queryByText("Invalid Date")).toBeNull();
  fetchMock.mockRestore();
});

test("daemon panel requires explicit confirmation for RP-ID changes", async () => {
  const fetchMock = vi
    .spyOn(globalThis, "fetch")
    // initial GET /api/settings
    .mockResolvedValueOnce(new Response(JSON.stringify({ hostname: "old.example", extraSans: "", port: "8686" })))
    // PUT refused: RP ID change with registered passkeys
    .mockResolvedValueOnce(
      new Response(JSON.stringify({ error: "would change the WebAuthn RP ID", rpChange: true, credentials: 2 }), {
        status: 409,
      }),
    )
    // confirmed PUT
    .mockResolvedValueOnce(new Response(JSON.stringify({ ok: true, rpWarning: true, restartRequired: true })))
    // refresh GET /api/settings
    .mockResolvedValueOnce(new Response(JSON.stringify({ hostname: "new.example", extraSans: "", port: "8686" })));

  render(<DaemonPanel />);
  const hostInput = await screen.findByDisplayValue("old.example");
  await userEvent.clear(hostInput);
  await userEvent.type(hostInput, "new.example");
  await userEvent.click(screen.getByText("Save"));

  // First PUT must not pre-confirm.
  await waitFor(() => {
    const puts = fetchMock.mock.calls.filter(([, init]) => (init as RequestInit)?.method === "PUT");
    expect(puts).toHaveLength(1);
  });
  const firstPut = fetchMock.mock.calls.filter(([, init]) => (init as RequestInit)?.method === "PUT")[0];
  expect(JSON.parse((firstPut[1] as RequestInit).body as string).confirmRpChange).toBeFalsy();

  // The refusal surfaces a confirm action; nothing was written yet.
  const confirmButton = await screen.findByText("Confirm hostname change");
  await userEvent.click(confirmButton);

  await waitFor(() => {
    const puts = fetchMock.mock.calls.filter(([, init]) => (init as RequestInit)?.method === "PUT");
    expect(puts).toHaveLength(2);
  });
  const secondPut = fetchMock.mock.calls.filter(([, init]) => (init as RequestInit)?.method === "PUT")[1];
  expect(JSON.parse((secondPut[1] as RequestInit).body as string).confirmRpChange).toBe(true);
  fetchMock.mockRestore();
});

test("daemon panel surfaces validation errors", async () => {
  const fetchMock = vi
    .spyOn(globalThis, "fetch")
    .mockResolvedValueOnce(new Response(JSON.stringify({ hostname: "old.example", extraSans: "", port: "8686" })))
    .mockResolvedValueOnce(
      new Response(JSON.stringify({ error: 'hostname "dotless" must contain a dot' }), { status: 400 }),
    );

  render(<DaemonPanel />);
  const hostInput = await screen.findByDisplayValue("old.example");
  await userEvent.clear(hostInput);
  await userEvent.type(hostInput, "dotless");
  await userEvent.click(screen.getByText("Save"));

  await screen.findByText(/must contain a dot/);
  fetchMock.mockRestore();
});

test("daemon panel shows the daemon version", async () => {
  const fetchMock = vi
    .spyOn(globalThis, "fetch")
    .mockResolvedValueOnce(
      new Response(JSON.stringify({ hostname: "host.local", extraSans: "", port: "8686", version: "1.4.0" })),
    );

  render(<DaemonPanel />);
  expect(await screen.findByText("1.4.0")).toBeInTheDocument();
  fetchMock.mockRestore();
});

test("daemon panel sends port as a string", async () => {
  const fetchMock = vi
    .spyOn(globalThis, "fetch")
    // initial GET /api/settings
    .mockResolvedValueOnce(new Response(JSON.stringify({ hostname: "host.local", extraSans: "san1", port: "8443" })))
    // PUT /api/settings
    .mockResolvedValueOnce(new Response(JSON.stringify({ ok: true, rpWarning: false, restartRequired: true })))
    // refresh GET /api/settings
    .mockResolvedValueOnce(new Response(JSON.stringify({ hostname: "host.local", extraSans: "san1", port: "9443" })));

  render(<DaemonPanel />);
  const portInput = await screen.findByDisplayValue("8443");
  await userEvent.clear(portInput);
  await userEvent.type(portInput, "9443");
  await userEvent.click(screen.getByText("Save"));

  await waitFor(() => {
    const putCall = fetchMock.mock.calls.find(([, init]) => (init as RequestInit)?.method === "PUT");
    expect(putCall).toBeDefined();
  });
  const putCall = fetchMock.mock.calls.find(([, init]) => (init as RequestInit)?.method === "PUT")!;
  const body = JSON.parse((putCall[1] as RequestInit).body as string);
  expect(body.port).toBe("9443");
  expect(typeof body.port).toBe("string");
  fetchMock.mockRestore();
});

test("a failed tools fetch shows an error with a working Retry, not an empty list", async () => {
  const fetchMock = vi
    .spyOn(globalThis, "fetch")
    .mockRejectedValueOnce(new TypeError("Failed to fetch"))
    .mockResolvedValueOnce(new Response(JSON.stringify([{ id: 1, name: "zsh", command: "zsh" }])));

  render(<ToolsPanel />);
  expect(await screen.findByText("Can't reach the daemon.")).toBeInTheDocument();
  // The "nothing configured" note must not stand in for a failed request.
  expect(screen.queryByText(/No tools yet/)).toBeNull();

  await userEvent.click(screen.getByText("Retry"));

  await screen.findByText((content, element) => content === "zsh" && element?.tagName === "TD");
  expect(screen.queryByText("Can't reach the daemon.")).toBeNull();
  expect(fetchMock).toHaveBeenCalledTimes(2);
  fetchMock.mockRestore();
});

test("a 500 on the passkey list is reported, not shown as zero passkeys", async () => {
  const fetchMock = vi
    .spyOn(globalThis, "fetch")
    .mockResolvedValueOnce(new Response(JSON.stringify({ error: "credential store unreadable" }), { status: 500 }));

  render(<PasskeysPanel />);
  expect(await screen.findByText(/credential store unreadable/)).toBeInTheDocument();
  expect(screen.queryByText(/No passkeys registered/)).toBeNull();
  fetchMock.mockRestore();
});

test("a failed daemon-settings fetch reports the error instead of spinning forever", async () => {
  const fetchMock = vi.spyOn(globalThis, "fetch").mockRejectedValueOnce(new TypeError("Failed to fetch"));

  render(<DaemonPanel />);
  expect(await screen.findByText("Can't reach the daemon.")).toBeInTheDocument();
  expect(screen.getByText("Retry")).toBeInTheDocument();
  expect(screen.queryByText("Save")).toBeNull();
  fetchMock.mockRestore();
});
