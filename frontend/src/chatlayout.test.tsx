import { describe, expect, it } from "vitest";
import appCss from "./styles/app.css?raw";

describe("chat log layout", () => {
  it("never lets a message or widget shrink inside the scrolling log", () => {
    // .chat is a scrolling flex column; a child with overflow:hidden (a widget) has min-height 0
    // and collapsed to its border when the log was taller than the panel.
    expect(appCss).toMatch(/\.chat\s*>\s*\*\s*\{[^}]*flex-shrink:\s*0/);
    expect(appCss).toMatch(/\.chat\{[^}]*flex-direction:column/);
  });
});
