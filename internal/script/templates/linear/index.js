// Linear through an MCP server from your Claude config. Tool names follow Linear's MCP server.
function server() { return host.config.server || "linear"; }

function tool(name, args) {
  var r = host.mcp.call(server(), name, args);
  if (r && r.is_error) {
    throw new Error("Linear " + name + " failed: " + String(r.text || "").slice(0, 300));
  }
  return r && r.json !== undefined ? r.json : r;
}

function nameOf(v) {
  if (v === null || v === undefined) return "";
  if (typeof v === "object") return String(v.name || v.displayName || "");
  return String(v);
}

function toItem(i) {
  var id = i.identifier || i.id;
  return {
    item_id: String(id),
    title: id + " " + (i.title || ""),
    url: i.url || "",
    fields: {
      status: nameOf(i.status || i.state),
      title: i.title || "",
      assignee: nameOf(i.assignee),
      priority: nameOf(i.priority)
    }
  };
}

var ops = {
  search: function (input) {
    var res = tool("list_issues", { query: input.q, limit: 20 });
    var list = Array.isArray(res) ? res : (res.issues || res.nodes || []);
    return { items: list.map(toItem) };
  },

  get: function (input) {
    return toItem(tool("get_issue", { id: input.item_id }));
  },

  update: function (input) {
    if (input.field !== "status") {
      throw new Error("linear: only the status can be changed from Hadron, not " + input.field);
    }
    return { fields: toItem(tool("save_issue", { id: input.item_id, state: String(input.value) })).fields };
  },

  poll: function (input) {
    return { items: input.items.map(function (it) { return ops.get({ item_id: it.item_id }); }) };
  }
};
