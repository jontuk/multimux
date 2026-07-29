import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi, afterEach, beforeEach } from "vitest";
import LoginPage from "../pages/LoginPage";

vi.mock("../webauthn", () => ({ login: vi.fn().mockResolvedValue(undefined) }));
vi.mock("../servers", () => ({ localServer: () => ({ origin: "https://local" }) }));

const realLocation = window.location;
const realSecureContext = Object.getOwnPropertyDescriptor(window, "isSecureContext");

function stubLocation(hash: string, pathname = "/", search = "") {
  const reload = vi.fn();
  Object.defineProperty(window, "location", {
    configurable: true,
    value: { hash, pathname, search, reload },
  });
  return reload;
}

function setSecureContext(value: boolean) {
  Object.defineProperty(window, "isSecureContext", { configurable: true, value });
}

beforeEach(() => {
  vi.clearAllMocks();
});

afterEach(() => {
  Object.defineProperty(window, "location", { configurable: true, value: realLocation });
  if (realSecureContext) Object.defineProperty(window, "isSecureContext", realSecureContext);
  else Reflect.deleteProperty(window, "isSecureContext");
});

test("login redirects to root when no connect route pending", async () => {
  const reload = stubLocation("");
  render(<LoginPage />);
  await userEvent.click(screen.getByText("Sign in with passkey"));
  await vi.waitFor(() => expect(reload).toHaveBeenCalled());
  expect(window.location.hash).toBe("#/");
});

test("login preserves a pending connect route", async () => {
  const reload = stubLocation("#/connect?opener=abc");
  render(<LoginPage />);
  await userEvent.click(screen.getByText("Sign in with passkey"));
  await vi.waitFor(() => expect(reload).toHaveBeenCalled());
  expect(window.location.hash).toBe("#/connect?opener=abc");
});

test("an insecure login keeps the wordmark but hides passkey controls behind the trust prompt", () => {
  stubLocation("#/connect?opener=abc", "/login", "?from=phone");
  setSecureContext(false);

  render(<LoginPage />);

  expect(screen.getByText("multimux")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Sign in with passkey" })).toBeNull();
  const trustLink = screen.getByRole("link", { name: "Install the Android CA" });
  expect(trustLink).toHaveAttribute("href", "/trust?return=%2Flogin%3Ffrom%3Dphone%23%2Fconnect%3Fopener%3Dabc");
  expect(new URLSearchParams(trustLink.getAttribute("href")?.split("?")[1]).get("return")).toBe(
    "/login?from=phone#/connect?opener=abc",
  );
});

test("a secure login keeps the passkey controls", () => {
  setSecureContext(true);

  render(<LoginPage />);

  expect(screen.getByText("multimux")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Sign in with passkey" })).toBeInTheDocument();
  expect(screen.queryByRole("link", { name: "Install the Android CA" })).toBeNull();
});
