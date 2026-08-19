import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, vi } from "vitest";
import App from "../App";

const health = { status: "ok", setupPending: false, version: "1.0.0" };
const realSecureContext = Object.getOwnPropertyDescriptor(window, "isSecureContext");
const realVisualViewport = Object.getOwnPropertyDescriptor(window, "visualViewport");

function setSecureContext(value: boolean) {
  Object.defineProperty(window, "isSecureContext", { configurable: true, value });
}

function setVisualViewportHeight(height: number) {
  const viewport = new EventTarget();
  Object.defineProperty(viewport, "height", { configurable: true, value: height });
  Object.defineProperty(window, "visualViewport", { configurable: true, value: viewport });
}

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
  window.history.replaceState({}, "", "/");
  if (realSecureContext) Object.defineProperty(window, "isSecureContext", realSecureContext);
  else Reflect.deleteProperty(window, "isSecureContext");
  if (realVisualViewport) Object.defineProperty(window, "visualViewport", realVisualViewport);
  else Reflect.deleteProperty(window, "visualViewport");
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

test("the ready shell marks only the grid route and keeps Settings accessible when its text collapses", async () => {
  stubFetch(ok, () => Promise.resolve(new Response(JSON.stringify({ name: "jon" }))));
  render(<App />);

  const gridLink = await screen.findByRole("link", { name: "Grid" });
  const settingsLink = within(screen.getByRole("navigation")).getByRole("link", { name: "Settings" });
  const app = gridLink.closest(".app");

  expect(app).toHaveClass("grid-route");
  expect(settingsLink).toHaveAttribute("aria-label", "Settings");
  expect(settingsLink.querySelector(".settings-icon")).toHaveAttribute("aria-hidden", "true");
  expect(settingsLink.querySelector(".nav-text")).toHaveTextContent("Settings");

  window.location.hash = "#/settings";
  window.dispatchEvent(new HashChangeEvent("hashchange"));

  await waitFor(() => expect(app).not.toHaveClass("grid-route"));
});

test("the ready shell publishes the initial visual viewport height", async () => {
  setVisualViewportHeight(612);
  stubFetch(ok, () => Promise.resolve(new Response(JSON.stringify({ name: "jon" }))));
  render(<App />);

  const gridLink = await screen.findByRole("link", { name: "Grid" });
  expect(gridLink.closest(".app")).toHaveStyle("--mobile-viewport-height: 612px");
});

test("insecure setup keeps the wordmark but hides registration controls behind the trust prompt", async () => {
  window.history.replaceState({}, "", "/setup?code=ABC123");
  setSecureContext(false);
  stubFetch(
    () => Promise.resolve(new Response(JSON.stringify({ ...health, setupPending: true }))),
    () => Promise.resolve(new Response("", { status: 401 })),
  );

  render(<App />);

  expect(await screen.findByText("multimux")).toBeInTheDocument();
  expect(screen.queryByLabelText("Your name")).toBeNull();
  expect(screen.queryByLabelText("Passkey name")).toBeNull();
  expect(screen.queryByRole("button", { name: "Register passkey" })).toBeNull();
  expect(screen.getByRole("link", { name: "Install the Android CA" })).toHaveAttribute(
    "href",
    "/trust?return=%2Fsetup%3Fcode%3DABC123",
  );
});

test("secure setup keeps the registration controls", async () => {
  window.history.replaceState({}, "", "/setup?code=ABC123");
  setSecureContext(true);
  stubFetch(
    () => Promise.resolve(new Response(JSON.stringify({ ...health, setupPending: true }))),
    () => Promise.resolve(new Response("", { status: 401 })),
  );

  render(<App />);

  expect(await screen.findByText("multimux")).toBeInTheDocument();
  expect(screen.getByLabelText("Your name")).toBeInTheDocument();
  expect(screen.getByLabelText("Passkey name")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Register passkey" })).toBeInTheDocument();
  expect(screen.queryByRole("link", { name: "Install the Android CA" })).toBeNull();
});
