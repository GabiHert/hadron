# Dashboard

A dashboard is a list of blocks per workspace. Read it with `dash_get` (each block comes with its data resolved). Change it with `dash_block_add`, `dash_block_update`, `dash_block_move`, `dash_block_remove`, or replace it with `dash_set`.

Block types: `stats`, `bars`, `heatmap` (groups queries), `table` (groups or rows), `activity` (rows), `prose` (text), `planner` (text you write), `custom` (HTML widget).

**Widgets from a chat.** A chat widget reaches the dashboard only when the user decides: they pick "Add to dashboard" in the widget's "⋯" menu, or ask the planner, who then calls `widget_pin {message_id}` (sessions cannot). Do not pin a chat widget on your own, and do not post widgets in chat to fill the dashboard: they are there to explain. To build a dashboard block on purpose, use `dash_block_add` with type `custom`.

Named queries (`source: {query, args}`): `cards.count {by: column | <field path>, expr?}`, `cards.list {expr?}`, `questions.open`, `mcp.calls {since, by?}` (by: actor, tool, hour, ok), `rules.fired {since}`, and `<integration>.links` / `<integration>.count {by}`. `dash_query_run` tests a query.

**Heatmap layouts.** A `heatmap` block draws a strip by default (one square per group, as for `mcp.calls {by: hour}`). With `source.layout: "calendar"`, or automatically when every group key is a date written `YYYY-MM-DD`, it draws a contributions-style grid: weeks as columns, Monday to Sunday as rows, month labels on top, five colour steps and a Less/More legend, with empty squares for days that have no group. Options: `weeks` (1 to 53, default 26) and `unit` (the noun for tooltips and the total line, such as "deploys"). The total line reads "N deploys in the last 26 weeks".

**Queries from integrations.** Besides the built-ins, an integration's script can define queries (`<integration>.<name>`, see the integrations topic); they accept any args and are served from a cache, so a block can be pending at first, show how old its data is, or show the error of the last refresh next to the last good data.

A **planner block** is a summary you write: when asked (it arrives as "[dashboard] Write the text for ..."), call `dash_block_update {block_id, text}` with two to four plain sentences. Do not answer in chat.
