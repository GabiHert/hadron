import manifestRaw from "../../internal/actions/testdata/actions.json?raw";
import * as api from "./api";

const invoke = vi.fn();
vi.mock("../wailsjs/go/app/App", () => ({ Invoke: (...a: unknown[]) => invoke(...a) }));
const handlers: Record<string, (e: unknown) => void> = {};
vi.mock("../wailsjs/runtime/runtime", () => ({
  EventsOn: (name: string, cb: (e: unknown) => void) => {
    handlers[name] = cb;
    return () => delete handlers[name];
  },
}));

beforeEach(() => invoke.mockReset());

describe("api.ts covers the registry", () => {
  const manifest: { name: string; fields: { name: string; required: boolean }[] }[] = JSON.parse(manifestRaw);
  it("lists the actions", () => expect(manifest.length).toBeGreaterThan(20));
  it.each(manifest.map((m) => m.name))("exports a wrapper named %s that invokes that action", async (name) => {
    const fn = (api as unknown as Record<string, (a?: object) => Promise<unknown>>)[name];
    expect(typeof fn).toBe("function");
    invoke.mockResolvedValue("{}");
    await fn({});
    expect(invoke).toHaveBeenCalledWith(name, "{}");
  });
});

describe("argument shaping", () => {
  it("sends card_move without a position when none is given, and never null", async () => {
    invoke.mockResolvedValue("{}");
    await api.card_move({ card_id: "c1", column_id: "col" });
    expect(invoke).toHaveBeenCalledWith("card_move", '{"card_id":"c1","column_id":"col"}');
    await api.card_move({ card_id: "c1", column_id: "col", position: 0, });
    expect(invoke).toHaveBeenLastCalledWith("card_move", '{"card_id":"c1","column_id":"col","position":0}');
  });

  it("drops undefined optionals", async () => {
    invoke.mockResolvedValue("{}");
    await api.column_update({ column_id: "x", name: undefined, wip: 0 });
    expect(invoke).toHaveBeenCalledWith("column_update", '{"column_id":"x","wip":0}');
  });

  it("parses the result and surfaces Go errors as Error", async () => {
    invoke.mockResolvedValueOnce('[{"id":"w1"}]');
    expect(await api.workspace_list()).toEqual([{ id: "w1" }]);
    invoke.mockRejectedValueOnce(new Error('card "x" does not exist'));
    await expect(api.card_get({ card_id: "x" })).rejects.toThrow('card "x" does not exist');
  });

  it("subscribes to the changed event and unsubscribes", () => {
    const seen: unknown[] = [];
    const off = api.onChanged((e) => seen.push(e));
    handlers.changed({ workspace_id: "w1", entities: ["cards"] });
    expect(seen).toEqual([{ workspace_id: "w1", entities: ["cards"] }]);
    off();
    expect(handlers.changed).toBeUndefined();
  });
});
