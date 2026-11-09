// A blank integration. Every op is a function on ops that takes the input and returns the output.
// Reach the outside world only through host: host.http.request, host.secret, host.exec, host.mcp.call,
// host.log, host.config. Declare what you use under permissions in manifest.json first.
var ops = {
  get: function (input) {
    host.log("get", input.item_id);
    return { item_id: input.item_id, title: "Item " + input.item_id, url: "", fields: { status: "open" } };
  }
};
