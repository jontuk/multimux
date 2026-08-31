import { render, screen } from "@testing-library/react";
import { vi } from "vitest";
import App from "../App";

test("renders app shell", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = String(input);
    if (url.includes("/healthz")) return new Response(JSON.stringify({ status: "ok", setupPending: false }));
    if (url.includes("/api/auth/me")) return new Response(JSON.stringify({ name: "jon" }));
    return new Response("[]");
  });

  render(<App />);
  expect(await screen.findByRole("link", { name: "Grid" })).toBeInTheDocument();
  expect(screen.getByText("multimux")).toBeInTheDocument();
});
