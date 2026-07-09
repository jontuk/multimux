import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi, afterEach, beforeEach } from "vitest";
import LoginPage from "../pages/LoginPage";

vi.mock("../webauthn", () => ({ login: vi.fn().mockResolvedValue(undefined) }));
vi.mock("../servers", () => ({ localServer: () => ({ origin: "https://local" }) }));

const realLocation = window.location;

function stubLocation(hash: string) {
  const reload = vi.fn();
  Object.defineProperty(window, "location", {
    configurable: true,
    value: { hash, reload },
  });
  return reload;
}

beforeEach(() => {
  vi.clearAllMocks();
});

afterEach(() => {
  Object.defineProperty(window, "location", { configurable: true, value: realLocation });
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
