import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, vi } from "vitest";
import App from "../App";

const health = { status: "ok", setupPending: false, version: "1.0.0" };

/** Answers /healthz from `healthz` and /api/auth/me from `me`. */
function stubFetch(healthz: () => Promise<Response>, me: () => Promise<Response>) {
  return vi.spyOn(globalThis, "fetch").mockImplementation((input: RequestInfo | URL) => {
    const url = String(input);
    if (url.includes("/healthz")) return healthz();
    if (url.includes("/api/auth/me")) return me();
    return Promise.resolve(new Response("[]"));
  });
}

const ok = () => Promise.resolve(new Response(JSON.stringify(health)));
const offline = () => Promise.reject(new TypeError("Failed to fetch"));

afterEach(() => {
  vi.restoreAllMocks();
});

test("a network failure on /api/auth/me shows the unreachable screen, not the login page", async () => {
  stubFetch(ok, offline);
  render(<App />);
  expect(await screen.findByText(/Can't reach the multimux daemon/)).toBeInTheDocument();
  expect(screen.queryByText("Sign in with passkey")).toBeNull();
});

test("an unreachable /healthz shows the unreachable screen", async () => {
  stubFetch(offline, offline);
  render(<App />);
  expect(await screen.findByText(/Can't reach the multimux daemon/)).toBeInTheDocument();
  expect(screen.queryByText("Sign in with passkey")).toBeNull();
});

test("a 401 from /api/auth/me still shows the login page", async () => {
  stubFetch(ok, () => Promise.resolve(new Response("", { status: 401 })));
  render(<App />);
  expect(await screen.findByText("Sign in with passkey")).toBeInTheDocument();
  expect(screen.queryByText(/Can't reach the multimux daemon/)).toBeNull();
});

test("a 500 from /api/auth/me shows the daemon-error screen with its detail", async () => {
  stubFetch(ok, () =>
    Promise.resolve(new Response(JSON.stringify({ error: "user store unreadable" }), { status: 500 })),
  );
  render(<App />);
  expect(await screen.findByRole("heading", { name: "The daemon returned an error" })).toBeInTheDocument();
  expect(screen.getByText(/user store unreadable/)).toBeInTheDocument();
  expect(screen.queryByText("Sign in with passkey")).toBeNull();
});

test("Retry re-runs the startup check and lets the app through once the daemon answers", async () => {
  let up = false;
  stubFetch(
    () => (up ? ok() : offline()),
    () => (up ? Promise.resolve(new Response(JSON.stringify({ name: "jon" }))) : offline()),
  );
  render(<App />);
  await screen.findByText(/Can't reach the multimux daemon/);

  up = true;
  await userEvent.click(screen.getByText("Retry"));
  expect(await screen.findByRole("link", { name: "Grid" })).toBeInTheDocument();
});
