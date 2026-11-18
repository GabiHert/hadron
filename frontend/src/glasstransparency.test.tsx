// Settings → Appearance → Glass transparency: a slider that scales every glass surface's alpha,
// previewed while dragged, saved as ui.glass_transparency when let go.
import css from "./styles/app.css?raw";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { glassScale, MIN_GLASS_SCALE } from "./state/useGlass";
import { mocks, reset, state } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

beforeEach(() => {
  reset();
  delete document.documentElement.dataset.glass;
  document.documentElement.style.removeProperty("--glass-k");
});

const k = () => document.documentElement.style.getPropertyValue("--glass-k");

async function openAppearance() {
  const user = userEvent.setup();
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
  await user.click(screen.getByRole("button", { name: "Settings" }));
  await user.click(within(screen.getByRole("navigation", { name: "Settings sections" })).getByRole("button", { name: "Appearance" }));
  await screen.findByRole("heading", { name: "Appearance" });
  return user;
}
const slider = () => screen.getByRole("slider", { name: "Glass transparency" });

describe("glass transparency", () => {
  it("maps the slider to an alpha scale, as Go does, capped short of clear", () => {
    expect(glassScale(0)).toBe(1);
    expect(glassScale(60)).toBe(0.64);
    expect(glassScale(100)).toBe(MIN_GLASS_SCALE);
    expect(glassScale(500)).toBe(MIN_GLASS_SCALE);
    expect(glassScale(-3)).toBe(1);
  });

  it("is more see-through than the theme's glass by default", async () => {
    render(<App />);
    await screen.findByRole("heading", { name: "Acme" });
    await waitFor(() => expect(k()).toBe("0.64"));
  });

  it("previews while dragged and saves ui.glass_transparency when let go", async () => {
    await openAppearance();
    await waitFor(() => expect(slider()).toBeEnabled());
    expect(slider()).toHaveValue("60");
    fireEvent.change(slider(), { target: { value: "100" } });
    expect(k()).toBe("0.4");
    expect(screen.getByLabelText("Current glass transparency")).toHaveTextContent("100%");
    expect(mocks.settings_update).not.toHaveBeenCalledWith({ values: { "ui.glass_transparency": 100 } });
    fireEvent.pointerUp(slider());
    await waitFor(() => expect(mocks.settings_update).toHaveBeenCalledWith({ values: { "ui.glass_transparency": 100 } }));
    await waitFor(() => expect(slider()).toHaveValue("100"));
    expect(k()).toBe("0.4");
  });

  it("goes back to the default with its button", async () => {
    state.fx.settings["ui.glass_transparency"] = 20;
    const user = await openAppearance();
    await waitFor(() => expect(slider()).toHaveValue("20"));
    await user.click(screen.getByRole("button", { name: "Default (60%)" }));
    await waitFor(() => expect(mocks.settings_update).toHaveBeenCalledWith({ values: { "ui.glass_transparency": 60 } }));
    await waitFor(() => expect(k()).toBe("0.64"));
  });

  it("is disabled, and says why, while Reduce transparency keeps the glass solid", async () => {
    state.reduceTransparency = true;
    await openAppearance();
    await waitFor(() => expect(screen.getByText(/Reduce transparency keeps the glass solid/)).toBeInTheDocument());
    expect(slider()).toBeDisabled();
  });

  it("scales every glass surface, and Reduce transparency still makes them solid", () => {
    expect(css).toContain(":root{--glass-k:1;--glass-pane:color-mix(in srgb,var(--glass) calc(var(--glass-k) * 100%),transparent);--glass-veil:calc(.5 + var(--glass-k) * .5)}");
    // The panes, the floating header and composer, the top bar and its tab strip, the tutorial caption.
    expect(css).toMatch(/\.cpanel\)::before\{[^}]*background:var\(--glass-pane\)/);
    expect(css).toContain("background:color-mix(in srgb,var(--glass) calc(var(--glass-veil) * 70%),transparent)");
    expect(css).toContain("background:color-mix(in srgb,var(--side) calc(var(--glass-veil) * 58%),transparent)");
    expect(css).toContain(".wsbar{background:color-mix(in srgb,var(--card) calc(var(--glass-veil) * 55%),transparent)");
    expect(css).toMatch(/\.tour-cap\{[^}]*background:color-mix\(in srgb,var\(--glass,var\(--card\)\) calc\(var\(--glass-veil\) \* 100%\),transparent\)/);
    // The blur stays at every setting.
    expect(css).toMatch(/\.cpanel\)::before\{[^}]*backdrop-filter:blur\(24px\)/);
    // Solid and Reduce transparency do not use the scale at all.
    expect(css).toContain(':root[data-glass="solid"] :is(.ppanel,.cpanel)::before{background:var(--card)');
    expect(css).toMatch(/prefers-reduced-transparency: reduce\)\{\s*:root\[data-glass="on"\] :is\(\.ppanel,\.cpanel\)::before,[^{]*\{background:var\(--card\)/);
  });
});
