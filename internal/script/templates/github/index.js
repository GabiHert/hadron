// GitHub pull requests through the gh CLI, which must be signed in (gh auth login).
var JSON_FIELDS = "number,title,state,url,reviewDecision";

function gh(args) {
  var r = host.exec("gh", args);
  if (r.code !== 0) {
    throw new Error("gh " + args.slice(0, 2).join(" ") + " failed (" + r.code + "): " + String(r.stderr || r.stdout).slice(0, 300));
  }
  return r.stdout;
}

function toItem(pr) {
  return {
    item_id: String(pr.number),
    title: "#" + pr.number + " " + pr.title,
    url: pr.url,
    fields: { state: pr.state, review: pr.reviewDecision || "", title: pr.title }
  };
}

var ops = {
  search: function (input) {
    var out = gh(["pr", "list", "--repo", host.config.repo, "--search", input.q, "--state", "all", "--limit", "20", "--json", JSON_FIELDS]);
    return { items: JSON.parse(out).map(toItem) };
  },

  get: function (input) {
    return toItem(JSON.parse(gh(["pr", "view", input.item_id, "--repo", host.config.repo, "--json", JSON_FIELDS])));
  },

  poll: function (input) {
    return { items: input.items.map(function (it) { return ops.get({ item_id: it.item_id }); }) };
  },

  actions: {
    comment: function (input) {
      gh(["pr", "comment", input.item_id, "--repo", host.config.repo, "--body", input.text]);
      return { ok: true };
    }
  }
};
