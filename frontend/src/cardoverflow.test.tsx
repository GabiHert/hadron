import { render, screen, within } from "@testing-library/react";
import { CardFace } from "./components/Board";
import css from "./styles/app.css?raw";
import { card } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

// jsdom does no layout, so these tests check the two halves of the fix: the face carries the full
// text in a tooltip, and the stylesheet keeps every part of the face inside the card.
const STAGE = "PRs #244/#245 com evidência E2E — aguardando revisão do orientador antes do merge";
const LONG = "a-very-long-nick-without-any-spaces-that-cannot-wrap-anywhere-at-all";

const face = () =>
  render(
    <CardFace
      card={card("c1", "w1", "col1", LONG, 0, { key: "ACME-508-" + LONG, stage: STAGE, assignee: "Beta reviewers team", liveness: "waiting" })}
      chips={[{ path: "links.pr.status", label: "PR status", value: "awaiting E2E evidence on #244 and #245 " + LONG }]}
      model={{ id: "claude-opus-5-5", source: "workspace" }}
      selected={false}
      dragging={false}
      onOpen={() => {}}
      onDragStart={() => {}}
      onDragEnd={() => {}}
      onDrop={() => {}}
    />,
  );

describe("a card face never grows past its card", () => {
  it("keeps the full text of a long stage, field value, key and assignee in a tooltip", () => {
    face();
    const c = screen.getByRole("button", { name: `Open ${LONG}` });
    expect(within(c).getByTitle(`Stage: ${STAGE}`)).toHaveTextContent(STAGE);
    expect(within(c).getByTitle(/^PR status: awaiting E2E evidence/)).toBeInTheDocument();
    expect(within(c).getByTitle("ACME-508-" + LONG)).toHaveClass("key");
    expect(within(c).getByTitle("Beta reviewers team")).toBeInTheDocument();
  });

  it("clips the face and ends every chip, key and assignee in an ellipsis", () => {
    // The column list scrolled sideways when a chip was wider than the card.
    expect(css).toContain(".clist{overflow-x:hidden}");
    expect(css).toContain(".card{min-width:0;overflow:hidden}");
    expect(css).toContain(".card>*{min-width:0;max-width:100%}");
    expect(css).toContain(".chips>*{display:block;min-width:0;max-width:100%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}");
    expect(css).toContain(".card .l1>.key{display:block;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}");
    expect(css).toMatch(/\.card \.l1>\.hint\{min-width:0;max-width:40%;overflow:hidden;text-overflow:ellipsis/);
  });

  it("does the same on dashboard card groups and collapsed columns", () => {
    expect(css).toMatch(/\.grpc \.key\{display:block;flex:0 1 auto;min-width:0;[^}]*text-overflow:ellipsis/);
    expect(css).toMatch(/\.grpc \.l2>\.pill\{display:block;flex:0 1 auto;min-width:0;max-width:60%;overflow:hidden;text-overflow:ellipsis/);
    expect(css).toContain(".rail{min-width:0;overflow:hidden}");
    expect(css).toContain(".rail .rname{min-height:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}");
  });
});
