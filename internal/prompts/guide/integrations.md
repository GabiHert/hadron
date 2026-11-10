# Integrations

An integration is a manifest plus a JavaScript file that runs in a sandbox and can only do what its manifest permits (network patterns, secrets, binaries, Hadron API calls). Starters: `jira`, `linear`, `github`, `empty`.

Build one: `integration_scaffold {name, template}` then `integration_write_file` for `manifest.json` and `index.js`, `integration_validate`, `integration_test` (recorded tests with fixtures), and leave it for approval (`integration_approve`; the user always may, the planner depends on `planner_policy`). Storing secrets (`integration_secret_set`) is a user-only action, governed by `mcp.user_actions` (see `overview`).

Once enabled, its fields appear as `<integration>.<field>`; `link_add` ties cards to items; `poll_seconds` keeps snapshots fresh; rules can call a script with `run_action`.

## Cards from new items (discovery)
An integration can fill a column with cards for new items, such as the issues of the open sprint. Declare `"discover": {}` under `ops`, add `"card.create"` to `permissions.api`, and set `poll_seconds`. `ops.discover({limit})` returns `{items: [{item_id, title, key?, url?, fields?}]}`; it may return every matching item each time, because Hadron skips the ones a card already holds. The user (or the planner) switches it on per workspace: `integration_discover_set {name, enabled: true, column: "backlog", max_per_run: 10}`; `integration_discover_run {name}` runs it now. The cards have no session (the user starts them), are linked to their item, and are named from the key (`PROJ-123` → `proj-123`). An item is never made into a card twice, even after its card is archived; `forget: true` resets that. The `jira` template has a discover op that searches its `discover_jql` setting (the open sprint without Cancelled issues by default).

Any op can also make a card itself with `host.card.create({key, title, column?, item_id?, url?, fields?})` (needs `card.create` in `permissions.api`, at most 25 a run); an item a card already links returns that card with `created: false`.

## Dashboard queries
An integration can feed dashboard blocks with data it fetches itself. Declare each query in the manifest under `ops.queries` and define it in `index.js` as `ops.queries.<name> = function (args) { ... }`. It returns `{groups: [{key, label, count}], ttl_s?}` and registers the query `<integration>.<name>`, which `dash_get` lists and a block's `source.query` can use. `args` is whatever the block passes.

It runs in the same sandbox as every op: `host.http.request`, `host.secret`, `host.exec`, the manifest's permissions and a time limit of 10 seconds a run. A query that pages through a large API can ask for more: set `timeout_s` (1 to 180) on its manifest entry, `"queries": {"deploys": {"timeout_s": 120}}`. Time spent inside `host.http.request` and `host.exec` does not count against it, each exec still stops at 20 seconds and each HTTP call at 30. A run that times out keeps the last good data and shows the error. Only one refresh of a query runs at a time, and a long one never blocks other ops or the UI. The result is cached per args for 10 minutes (set `ttl_s` for another time). A dashboard render never waits for the network: it shows the last result with its age, refreshes in the background, and shows the error on the block when the last refresh failed. `links` and `count` are reserved names.

Example: successful workflow runs per day of a repository (`manifest.json` permissions: `network: ["https://api.github.com/*"]`, `secrets: ["token"]`; the manifest declares `"ops": {"queries": {"deploys_per_day": {"description": "Successful workflow runs per day"}}}`):
```js
var ops = {
  queries: {
    deploys_per_day: function (args) {
      var r = host.http.request({
        method: "GET",
        url: "https://api.github.com/repos/" + args.repo + "/actions/runs",
        query: { status: "success", per_page: 100 },
        headers: { Accept: "application/vnd.github+json" },
        auth: { type: "bearer", secret: host.secret("token") }
      });
      if (r.status >= 400) { throw new Error("GitHub answered " + r.status); }
      var perDay = {};
      r.json.workflow_runs.forEach(function (run) {
        var day = run.created_at.slice(0, 10);
        perDay[day] = (perDay[day] || 0) + 1;
      });
      return {
        groups: Object.keys(perDay).sort().map(function (d) { return { key: d, label: d, count: perDay[d] }; }),
        ttl_s: 900
      };
    }
  }
};
```
Then add a heatmap block with `source: {query: "ci.deploys_per_day", args: {repo: "acme/web"}, layout: "calendar", unit: "deploys"}`.
