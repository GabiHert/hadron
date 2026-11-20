import { getConfig } from "@testing-library/react";
import { ASYNC_UTIL_TIMEOUT_MS, scaled, waitScale } from "./test/waits";

test("HADRON_TEST_WAIT_SCALE scales the deadlines, and anything but a positive number is ignored", () => {
  expect(waitScale({})).toBe(1);
  expect(waitScale({ HADRON_TEST_WAIT_SCALE: "2" })).toBe(2);
  expect(scaled(10_000, { HADRON_TEST_WAIT_SCALE: "0.5" })).toBe(5_000);
  for (const bad of ["0", "-1", "fast", ""]) expect(waitScale({ HADRON_TEST_WAIT_SCALE: bad })).toBe(1);
});

test("the setup gives findBy and waitFor the generous deadline", () => {
  expect(getConfig().asyncUtilTimeout).toBe(scaled(ASYNC_UTIL_TIMEOUT_MS));
});
