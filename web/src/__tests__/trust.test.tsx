/// <reference types="node" />
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, test, vi } from "vitest";
import App from "../App";
import TrustPage, { safeReturnTarget } from "../pages/TrustPage";

const caInfo = {
  subject: "multimux local CA (mux)",
  permittedDNSDomains: ["mux.local", "mux.example.ts.net"],
  expires: "2036-07-28T11:34:56Z",
  sha256Fingerprint: "AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99",
};

const styles = readFileSync(resolve(process.cwd(), "src/index.css"), "utf8");

function setSecureContext(value: boolean) {
  Object.defineProperty(window, "isSecureContext", { configurable: true, value });
}

function setLocation(path: string) {
  window.history.replaceState({}, "", path);
}

function caResponse() {
  return Promise.resolve(new Response(JSON.stringify(caInfo), { status: 200 }));
}

afterEach(() => {
  vi.restoreAllMocks();
  setSecureContext(false);
  setLocation("/");
});

describe("safeReturnTarget", () => {
  test.each([
    ["/setup?code=ABC", "https://mux.local", "/setup?code=ABC"],
    ["https://mux.local/login#x", "https://mux.local", "/login#x"],
    ["//evil.test/x", "https://mux.local", "/"],
    ["https://evil.test/x", "https://mux.local", "/"],
    ["javascript:alert(1)", "https://mux.local", "/"],
    ["https://mux.local//evil.test/x", "https://mux.local", "/"],
    ["https://mux.local/\\\\evil.test/x", "https://mux.local", "/"],
  ])("maps %s against %s to %s", (raw, origin, expected) => {
    expect(safeReturnTarget(raw, origin)).toBe(expected);
  });
});

test("shows CA details, Android installation steps, and bootstrap security guidance", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(caResponse);
  setSecureContext(false);
  render(<TrustPage />);

  expect(screen.getByText("Loading certificate information…")).toBeInTheDocument();
  expect(await screen.findByText(caInfo.subject)).toBeInTheDocument();
  expect(screen.getByText("mux.local")).toBeInTheDocument();
  expect(screen.getByText("mux.example.ts.net")).toBeInTheDocument();
  expect(screen.getByText("28 July 2036 at 11:34 UTC")).toBeInTheDocument();
  expect(screen.getByText(caInfo.sha256Fingerprint)).toHaveClass("trust-fingerprint");
  expect(screen.getByRole("link", { name: "Download CA certificate" })).toMatchObject({
    className: "primary",
  });
  expect(screen.getByRole("link", { name: "Download CA certificate" })).toHaveAttribute("href", "/ca.crt");
  expect(screen.getByRole("link", { name: "Download CA certificate" })).toHaveAttribute("download", "multimux-ca.crt");

  expect(
    screen.getByText(
      /Settings → Security & privacy → More security settings → Encryption & credentials → Install a certificate → CA certificate/,
    ),
  ).toBeInTheDocument();
  expect(screen.getByText(/Samsung.*Install from device storage/i)).toBeInTheDocument();
  expect(screen.getByText(/LAN, VPN, or tailnet you control/i)).toBeInTheDocument();
  expect(screen.getByText(/value printed by the daemon/i)).toBeInTheDocument();
  expect(screen.getByText(/do not continue/i)).toBeInTheDocument();
  expect(screen.getByText(/managed device.*user-added CAs/i)).toBeInTheDocument();
  expect(screen.getByText(/SSH.*USB or Quick Share/i)).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Reload and check trust" })).toBeInTheDocument();
});

test("keeps trust metadata within the card and makes the certificate download a primary tap target", () => {
  expect(styles).toMatch(/\.trust-page \.trust-fingerprint\s*\{[^}]*overflow-wrap:\s*anywhere;/s);
  expect(styles).toMatch(
    /\.trust-page \.auth-card a\.primary\s*\{[^}]*(?:display:\s*flex;[^}]*width:\s*100%;|width:\s*100%;[^}]*display:\s*flex;)[^}]*min-height:\s*44px;/s,
  );
});

test("retries loading CA information after an error", async () => {
  let attempts = 0;
  vi.spyOn(globalThis, "fetch").mockImplementation(() => {
    attempts += 1;
    return attempts === 1 ? Promise.resolve(new Response("temporarily unavailable", { status: 500 })) : caResponse();
  });
  render(<TrustPage />);

  expect(await screen.findByText(/could not load certificate information/i)).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Retry" }));
  expect(await screen.findByText(caInfo.subject)).toBeInTheDocument();
  expect(attempts).toBe(2);
});

test("shows secure-context success with a sanitized Continue destination", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(caResponse);
  setSecureContext(true);
  setLocation("/trust?return=https%3A%2F%2Fevil.test%2Fsteal");
  render(<TrustPage />);

  expect(await screen.findByRole("heading", { name: "Android now trusts this multimux daemon" })).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Continue" })).toHaveAttribute("href", "/");
  expect(screen.queryByRole("button", { name: "Reload and check trust" })).toBeNull();
});

test("routes /trust before setup and authentication startup screens", async () => {
  setLocation("/trust?return=%2Fsetup%3Fcode%3DABC");
  setSecureContext(true);
  vi.spyOn(globalThis, "fetch").mockImplementation((input) => {
    const url = String(input);
    if (url.includes("/ca/info")) return caResponse();
    if (url.includes("/healthz")) {
      return Promise.resolve(
        new Response(JSON.stringify({ status: "ok", setupPending: true, version: "1.0.0" }), { status: 200 }),
      );
    }
    if (url.includes("/api/auth/me")) return Promise.resolve(new Response("", { status: 401 }));
    return Promise.resolve(new Response("{}"));
  });

  render(<App />);

  expect(await screen.findByRole("heading", { name: "Trust multimux on Android" })).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Continue" })).toHaveAttribute("href", "/setup?code=ABC");
  await waitFor(() => expect(screen.queryByText("Register passkey")).toBeNull());
  expect(screen.queryByText("Sign in with passkey")).toBeNull();
});
