// Jira Cloud. Needs the "token" secret (an Atlassian API token) and the site and email settings.
var FIELDS = "summary,status,assignee,priority";

function base() { return "https://" + host.config.site + "/rest/api/3"; }

function call(method, path, opts) {
  opts = opts || {};
  var r = host.http.request({
    method: method,
    url: base() + path,
    query: opts.query,
    json: opts.json,
    headers: { Accept: "application/json" },
    auth: { type: "basic", user: host.config.email, secret: host.secret("token") }
  });
  if (r.status >= 400) {
    throw new Error("Jira " + method + " " + path + " answered " + r.status + ": " + String(r.text || "").slice(0, 300));
  }
  return r.json;
}

function toItem(issue) {
  var f = issue.fields || {};
  return {
    item_id: issue.key,
    title: issue.key + " " + (f.summary || ""),
    url: "https://" + host.config.site + "/browse/" + issue.key,
    fields: {
      status: f.status ? f.status.name : "",
      summary: f.summary || "",
      assignee: f.assignee ? f.assignee.displayName : "",
      priority: f.priority ? f.priority.name : ""
    }
  };
}

var ops = {
  search: function (input) {
    var q = String(input.q).replace(/"/g, '\\"');
    var jql = /^[A-Za-z][A-Za-z0-9]+-[0-9]+$/.test(input.q) ? 'key = "' + input.q.toUpperCase() + '"' : 'text ~ "' + q + '" ORDER BY updated DESC';
    var res = call("GET", "/search/jql", { query: { jql: jql, fields: FIELDS, maxResults: 20 } });
    return { items: (res.issues || []).map(toItem) };
  },

  get: function (input) {
    return toItem(call("GET", "/issue/" + input.item_id, { query: { fields: FIELDS } }));
  },

  update: function (input) {
    if (input.field !== "status") {
      throw new Error("jira: only the status can be changed from Hadron, not " + input.field);
    }
    var list = (call("GET", "/issue/" + input.item_id + "/transitions") || {}).transitions || [];
    var want = String(input.value).toLowerCase();
    var hit = list.filter(function (t) {
      return (t.to && String(t.to.name).toLowerCase() === want) || String(t.name).toLowerCase() === want;
    })[0];
    if (!hit) {
      throw new Error('Jira has no transition to "' + input.value + '" from the issue\'s current status. Available: ' +
        list.map(function (t) { return t.to ? t.to.name : t.name; }).join(", "));
    }
    call("POST", "/issue/" + input.item_id + "/transitions", { json: { transition: { id: hit.id } } });
    return { fields: toItem(call("GET", "/issue/" + input.item_id, { query: { fields: FIELDS } })).fields };
  },

  poll: function (input) {
    return { items: input.items.map(function (it) { return ops.get({ item_id: it.item_id }); }) };
  },

  // Discovery: the issues of discover_jql (the open sprint by default) for Hadron to make cards of.
  // Hadron skips the ones a card already holds, so this may return the whole sprint every time.
  discover: function (input) {
    var jql = host.config.discover_jql || "sprint in openSprints() AND status != Cancelled ORDER BY rank";
    var res = call("GET", "/search/jql", { query: { jql: jql, fields: FIELDS, maxResults: Math.min(100, Math.max(input.limit || 50, 50)) } });
    return {
      items: (res.issues || []).map(function (issue) {
        var it = toItem(issue);
        return { item_id: it.item_id, key: issue.key, title: it.fields.summary || issue.key, url: it.url, fields: it.fields };
      })
    };
  },

  actions: {
    comment: function (input) {
      var body = { type: "doc", version: 1, content: [{ type: "paragraph", content: [{ type: "text", text: input.text }] }] };
      var res = call("POST", "/issue/" + input.item_id + "/comment", { json: { body: body } });
      return { comment_id: res.id };
    }
  }
};
