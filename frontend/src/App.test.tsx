import { render, screen } from "@testing-library/react";
import App from "./App";
import { reset } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

test("the header shows the Hadron mark and wordmark", async () => {
  reset();
  render(<App />);
  expect(screen.getByRole("img", { name: "Hadron" })).toBeInTheDocument();
  expect(screen.getByText("Hadron")).toBeInTheDocument();
  expect(await screen.findByRole("heading", { name: "Acme" })).toBeInTheDocument();
});
