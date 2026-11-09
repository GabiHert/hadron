import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { ErrorBoundary } from "./components/ErrorBoundary";
import { defaultFixture, mocks, reset } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

beforeEach(() => reset());

function Boom(): never {
  throw new Error("kaboom");
}

describe("error and empty states", () => {
  it("the error boundary shows what happened and a way back", () => {
    const spy = vi.spyOn(console, "error").mockImplementation(() => undefined);
    render(<ErrorBoundary><Boom /></ErrorBoundary>);
    expect(screen.getByRole("alert")).toHaveTextContent("Something went wrong");
    expect(screen.getByRole("alert")).toHaveTextContent("kaboom");
    expect(screen.getByRole("button", { name: "Reload" })).toBeInTheDocument();
    spy.mockRestore();
  });

  it("renders its children when nothing is wrong", () => {
    render(<ErrorBoundary><p>fine</p></ErrorBoundary>);
    expect(screen.getByText("fine")).toBeInTheDocument();
  });

  it("explains a failed first load and retries", async () => {
    const user = userEvent.setup();
    mocks.workspace_list.mockRejectedValueOnce(new Error("database is locked"));
    render(<App />);
    expect(await screen.findByRole("alert")).toHaveTextContent("Hadron could not load its data: database is locked");
    await user.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("heading", { name: "Acme" })).toBeInTheDocument();
  });

  it("offers to create the first workspace when there is none", async () => {
    const user = userEvent.setup();
    mocks.workspace_list.mockResolvedValue([]);
    render(<App />);
    expect(await screen.findByText(/Create a workspace to start/)).toBeInTheDocument();
    await user.click(screen.getAllByRole("button", { name: /New workspace/ })[0]);
    expect(await screen.findByRole("dialog", { name: "New workspace" })).toBeInTheDocument();
    void defaultFixture;
  });
});
