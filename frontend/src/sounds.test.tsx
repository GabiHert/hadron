import { render, screen, waitFor, within, fireEvent } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { soundSettings } from "./components/SoundsTab";
import { mocks, reset, state } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

beforeEach(() => reset());

async function openSounds() {
  const user = userEvent.setup();
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
  await user.click(screen.getByRole("button", { name: "Settings" }));
  await user.click(within(screen.getByRole("navigation", { name: "Settings sections" })).getByRole("button", { name: "Sounds" }));
  await screen.findByRole("heading", { name: "Sounds" });
  return user;
}

describe("Settings → Sounds", () => {
  it("reads the defaults: on, 60%, nothing muted", () => {
    expect(soundSettings({})).toEqual({ enabled: true, volume: 0.6, muted: [] });
    expect(soundSettings({ "sounds.enabled": false, "sounds.volume": 0.2, "sounds.muted_workspaces": ["w2", 3] })).toEqual({ enabled: false, volume: 0.2, muted: ["w2"] });
  });

  it("switches sounds off, previews each one at the chosen volume, and mutes the workspace", async () => {
    const user = await openSounds();
    const on = screen.getByRole("checkbox", { name: "Play sounds" });
    await waitFor(() => expect(on).toBeChecked());
    await user.click(screen.getByRole("button", { name: "Preview Needs you" }));
    await waitFor(() => expect(mocks.sound_preview).toHaveBeenCalledWith({ sound: "needs_you", volume: 0.6 }));

    const vol = screen.getByRole("slider", { name: "Volume" });
    fireEvent.change(vol, { target: { value: "0.3" } });
    fireEvent.pointerUp(vol);
    await waitFor(() => expect(mocks.settings_update).toHaveBeenCalledWith({ values: { "sounds.volume": 0.3 } }));
    await user.click(screen.getByRole("button", { name: "Preview Back to work" }));
    await waitFor(() => expect(mocks.sound_preview).toHaveBeenCalledWith({ sound: "back_to_work", volume: 0.3 }));

    await user.click(screen.getByRole("checkbox", { name: "Mute Acme" }));
    await waitFor(() => expect(mocks.settings_update).toHaveBeenCalledWith({ values: { "sounds.muted_workspaces": ["w1"] } }));
    expect(screen.getByRole("checkbox", { name: "Mute Acme" })).toBeChecked();

    await user.click(on);
    await waitFor(() => expect(mocks.settings_update).toHaveBeenCalledWith({ values: { "sounds.enabled": false } }));
    expect(state.fx.settings["sounds.enabled"]).toBe(false);
    expect(screen.getByRole("slider", { name: "Volume" })).toBeDisabled();
  });
});
