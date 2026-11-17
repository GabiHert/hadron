# Widgets

A widget is a picture in the conversation. Use one whenever it explains something better than prose: a comparison, a before/after, a flow or an architecture, a timeline, numbers, a plan's steps, a status board. Write one or two lines of text with it that say what to look at, and do not repeat its content in prose. A one-line answer needs no widget.

`widget_post {channel, title, html}` puts a widget in a chat. A card session posts on its own card's channel; the planner and the user on any. The title is a short caption, such as "Retry flow" or "Before and after the fix".

**Widgets in chat are for explaining, not for the dashboard.** The user can add one to the dashboard from the widget's "⋯" menu, which also opens it full screen and copies its HTML. `widget_pin {message_id}` does the same for the planner, and only when the user asks for it. Sessions never pin.

## Three rules

1. **Use the kit.** Every frame ships the widget kit below: plain markup with its classes fits every theme, light, dark and custom. Reach for it first.
2. **Tokens only.** For anything the kit lacks, style with the theme tokens, such as `color:var(--fg2)`, `color:var(--ok)` or `border:1px solid var(--border)`. Never hard-code a colour (`#ccc`, `white`, `rgb(…)`), and never set a background on `html` or `body`: the frame already paints the card's background.
3. **Keep it narrow.** A card's chat is about 400px wide. Prefer stacked rows, a `dl.kv`, or 2 to 3 columns over a wide table. A table that is still too wide scrolls sideways.

## What the frame gives you

Before your HTML, every frame sets: `color-scheme` to the app's mode; every theme token as a real value (`--bg --card --muted --fg --fg2 --fg3 --border --row --acc --ok --okb --warn --warnb --bad --badb --sel` and more); the fonts `--sans --head --mono`; the body's background (`--card`), text colour (`--fg`) and font (13px `--sans`); `box-sizing:border-box`; images and SVG no wider than the frame; long words that wrap; tables at full width with wrapping cells. The frame is as tall as its content, and it follows a theme switch live. `theme_tokens` returns the current values, if you want to see them.

Rules of the frame: sandboxed iframe, no network, no cookies, no access to Hadron, CSP `default-src 'none'; style-src 'unsafe-inline'; img-src data:`. Your scripts are off unless the user turned them on, so do not rely on them. Keep it under 256 KB. In SVG, set colours with `style="fill:var(--fg)"` and the like.

## The kit, one example per class

### Table: `table.tbl`
Headers in `th`; `td.num` right-aligns a number.
```html
<table class="tbl">
  <tr><th>Step</th><th>Owner</th><th class="num">Rows</th></tr>
  <tr><td>Migrate</td><td>billing-fix</td><td class="num">1,204</td></tr>
  <tr><td>Backfill</td><td>invoice-sync</td><td class="num">88</td></tr>
</table>
```

### Stat tiles: `.stats` and `.stat`
A value in `b`, a label in `span`; `.stat.ok`, `.warn`, `.bad` or `.acc` colours the value.
```html
<div class="stats">
  <div class="stat ok"><b>612</b><span>tests pass</span></div>
  <div class="stat bad"><b>3</b><span>failing</span></div>
  <div class="stat"><b>41 s</b><span>suite time</span></div>
</div>
```

### Badge (pill): `.badge`
`.badge.ok`, `.badge.warn`, `.badge.bad`, `.badge.acc`, or a plain `.badge`.
```html
<span class="badge ok">merged</span> <span class="badge warn">review</span> <span class="badge bad">failing</span> <span class="badge">draft</span>
```

### Bars and meters: `.bar` and `.meter`
`.meter` is a track with an `<i>` fill at a width; `.meter.ok`, `.warn` or `.bad` colours it. `.bar` lays out a label, a meter and a number on one row.
```html
<div class="bar"><span>Done</span><div class="meter ok"><i style="width:70%"></i></div><b>7</b></div>
<div class="bar"><span>Blocked</span><div class="meter bad"><i style="width:20%"></i></div><b>2</b></div>
<div class="bar"><span>To do</span><div class="meter"><i style="width:10%"></i></div><b>1</b></div>
```

### Checklist: `ul.check`
`li.done` ✓, `li.now` ● (in progress), `li.warn` !, `li.fail` ✕, and a plain `li` is still to do.
```html
<ul class="check">
  <li class="done">Write the failing test</li>
  <li class="now">Run the full suite</li>
  <li class="warn">Update the docs <span class="muted">needs a decision</span></li>
  <li>Open the PR</li>
</ul>
```

### Key-value list: `dl.kv`
The narrow way to show a record; use it instead of a two-row wide table.
```html
<dl class="kv">
  <dt>Branch</dt><dd><code>fix/retry-loop</code></dd>
  <dt>Tests</dt><dd><span class="badge ok">612 pass</span></dd>
  <dt>Build</dt><dd>signed, 38 MB</dd>
</dl>
```

### Callout: `.callout`
A note with an accent bar; `.callout.ok`, `.warn` or `.bad` for its tone.
```html
<div class="callout warn"><b>Heads up</b> the migration locks the table for about 2 minutes.</div>
```

### Two-column compare: `.compare`
Two `section`s side by side, stacked when narrow; `section.ok`, `.warn` or `.bad` marks one.
```html
<div class="compare">
  <section class="bad"><h4>Before</h4><ul><li>p95 840 ms</li><li>41 queries a page</li></ul></section>
  <section class="ok"><h4>After</h4><ul><li>p95 210 ms</li><li>3 queries a page</li></ul></section>
</div>
```

Helpers: `.muted` (quiet text), `.small`, `.mono`, `.ok` `.warn` `.bad` `.acc` (coloured text), `.stack` (spaces its children), `.scroll` (a box that scrolls sideways). Classes combine: a `.stats` row over a `table.tbl` in a `.stack` is a status board.

## Diagrams in SVG
A widget that starts with `<svg viewBox="...">` gets the full chat width. Colour it with tokens: `style="fill:var(--fg)"`, `stroke:var(--border)`.
```html
<svg viewBox="0 0 400 110" width="100%" style="font:12px var(--sans)">
  <defs><marker id="a" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto"><path d="M0 0L10 5L0 10z" style="fill:var(--fg3)"/></marker></defs>
  <g style="stroke:var(--fg3);stroke-width:1.5;fill:none" marker-end="url(#a)"><path d="M100 40H140"/><path d="M250 40H290"/></g>
  <rect x="4" y="20" width="96" height="40" rx="8" style="fill:var(--card);stroke:var(--border)"/>
  <rect x="142" y="20" width="108" height="40" rx="8" style="fill:var(--card);stroke:var(--acc);stroke-width:2"/>
  <rect x="292" y="20" width="104" height="40" rx="8" style="fill:var(--card);stroke:var(--border)"/>
  <g style="fill:var(--fg)" text-anchor="middle"><text x="52" y="44">Request</text><text x="196" y="44">Queue</text><text x="344" y="44">Worker</text></g>
  <text x="200" y="92" text-anchor="middle" style="fill:var(--fg3);font-size:11px">retry on failure, up to 3 times</text>
</svg>
```

A sparkline:
```html
<svg viewBox="0 0 200 40" width="320"><polyline points="0,30 25,26 50,28 75,18 100,20 125,12 150,14 175,8 200,10" style="fill:none;stroke:var(--acc);stroke-width:2"/></svg>
```
