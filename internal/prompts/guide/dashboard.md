# Dashboard

A dashboard is a list of blocks per workspace. Read it with `dash_get` (each block comes with its data resolved). Change it with `dash_block_add`, `dash_block_update`, `dash_block_move`, `dash_block_remove`, or replace it with `dash_set`.

Block types: `stats`, `bars`, `heatmap` (groups queries), `table` (groups or rows), `activity` (rows), `prose` (text), `planner` (text you write), `custom` (HTML widget).

Named queries (`source: {query, args}`): `cards.count {by: column | <field path>, expr?}`, `cards.list {expr?}`, `questions.open`, `mcp.calls {since, by?}` (by: actor, tool, hour, ok), `rules.fired {since}`, and `<integration>.links` / `<integration>.count {by}`. `dash_query_run` tests a query.

A **planner block** is a summary you write: when asked (it arrives as "[dashboard] Write the text for ..."), call `dash_block_update {block_id, text}` with two to four plain sentences. Do not answer in chat.
