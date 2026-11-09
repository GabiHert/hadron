import { render, screen, within } from "@testing-library/react";
import App from "./App";
import { reset } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

test("the header shows no app name, mark or version", async () => {
  reset();
  render(<App />);
  expect(await screen.findByRole("heading", { name: "Acme" })).toBeInTheDocument();
  const header = screen.getByRole("banner");
  expect(within(header).queryByText("Hadron")).toBeNull();
  expect(within(header).queryByRole("img", { name: "Hadron" })).toBeNull();
  expect(screen.queryByTestId("version")).toBeNull();
  expect(within(header).getByRole("tablist", { name: "Workspaces" })).toBeInTheDocument();
});
