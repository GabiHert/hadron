import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { mocks, reset } from "./test/fakeApi";
import appCss from "./styles/app.css?raw";
import baseCss from "./styles/base.css?raw";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

beforeEach(() => reset());

async function openWithCard() {
  const user = userEvent.setup();
  let changed: (e: unknown) => void = () => {};
  mocks.onChanged.mockImplementation(((cb: (e: unknown) => void) => {
    changed = cb;
    return () => {};
  }) as never);
  const view = render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
  await user.click(await screen.findByRole("button", { name: "Open search-console" }));
  await screen.findByRole("complementary", { name: /Card search-console/ });
  return { user, view, fire: () => act(() => changed({ workspace_id: "w1", entities: ["workspaces", "cards"] })) };
}

test("the card panel collapses with a card selected, and stays collapsed across a changed event and a re-render", async () => {
  const { user, view, fire } = await openWithCard();
  await user.click(screen.getByRole("button", { name: "Collapse the card panel" }));
  await waitFor(() => expect(mocks.ui_panel_collapse).toHaveBeenCalledWith({ workspace_id: "w1", panel: "card", collapsed: true }));
  expect(await screen.findByRole("navigation", { name: "Card panel, collapsed" })).toBeInTheDocument();
  expect(screen.queryByRole("complementary", { name: /Card search-console/ })).toBeNull();

  fire();
  view.rerender(<App />);
  await waitFor(() => expect(mocks.workspace_list).toHaveBeenCalled());
  expect(screen.getByRole("navigation", { name: "Card panel, collapsed" })).toBeInTheDocument();

  // Expanding brings it back, and selecting another card does not undo a collapse.
  await user.click(screen.getByRole("button", { name: "Expand the card panel" }));
  expect(await screen.findByRole("complementary", { name: /Card search-console/ })).toBeInTheDocument();
});

test("selecting another card does not re-expand a collapsed card panel", async () => {
  const { user } = await openWithCard();
  await user.click(screen.getByRole("button", { name: "Collapse the card panel" }));
  await screen.findByRole("navigation", { name: "Card panel, collapsed" });
  const other = screen.getAllByRole("button", { name: /^Open / }).find((b) => !/search-console/.test(b.getAttribute("aria-label") ?? ""));
  expect(other).toBeTruthy();
  await user.click(other!);
  expect(screen.getByRole("navigation", { name: "Card panel, collapsed" })).toBeInTheDocument();
});

test("the planner panel collapses and stays collapsed across a changed event", async () => {
  const { user, fire } = await openWithCard();
  await user.click(screen.getByRole("button", { name: "Collapse the planner" }));
  expect(await screen.findByRole("navigation", { name: "Planner, collapsed" })).toBeInTheDocument();
  fire();
  await waitFor(() => expect(mocks.workspace_list).toHaveBeenCalled());
  expect(screen.getByRole("navigation", { name: "Planner, collapsed" })).toBeInTheDocument();
});

test("a draggable header does not make its buttons inert: they are marked no-drag", () => {
  const css = appCss + "\n" + baseCss;
  expect(css).toMatch(/\.top\{[^}]*--wails-draggable:\s*drag/);
  const noDrag = css.match(/([^{}]+)\{--wails-draggable:\s*no-drag\}/);
  expect(noDrag).not.toBeNull();
  for (const sel of [".top button", ".top input", ".top select", ".wsbar"]) expect(noDrag![1]).toContain(sel);
});
