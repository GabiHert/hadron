# Widgets

A widget is a picture in the conversation. Use one whenever it explains something better than prose: a comparison, a before/after, a flow or an architecture, a timeline, numbers, a plan's steps, a status board. Write one or two lines of text with it that say what to look at, and do not repeat its content in prose. A one-line answer needs no widget.

`widget_post {channel, title, html}` puts a widget in a chat. A card session posts on its own card's channel; the planner and the user on any. The title is a short caption, such as "Retry flow" or "Before and after the fix".

**Widgets in chat are for explaining, not for the dashboard.** The user can add one to the dashboard from the widget's "⋯" menu, which also opens it full screen and copies its HTML. `widget_pin {message_id}` does the same for the planner, and only when the user asks for it. Sessions never pin.

Rules of the frame: sandboxed iframe, no network, no cookies, no access to Hadron, CSP `default-src 'none'; style-src 'unsafe-inline'; img-src data:`. Scripts are off unless the user turned them on, so do not rely on them. Keep it under 256 KB. Use the theme's CSS variables so it works in light and dark: `--bg --card --fg --fg2 --fg3 --border --acc --ok --warn --bad --okb --warnb --badb --mono --sans --serif`. In SVG, set colours with `style="fill:var(--fg)"` and the like.

Sizing: with scripts off the frame cannot measure itself. A widget that starts with `<svg viewBox="...">` is sized exactly from its viewBox and the chat width, and gets the full width; anything else gets a guessed height and a "Show more" link. So start a diagram with the `<svg>` itself, and keep other widgets compact.

## A flow diagram (inline SVG, with arrows)
```html
<svg viewBox="0 0 460 120" width="100%" style="font:12px var(--sans)">
  <defs><marker id="a" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto"><path d="M0 0L10 5L0 10z" style="fill:var(--fg3)"/></marker></defs>
  <g style="stroke:var(--fg3);stroke-width:1.5;fill:none" marker-end="url(#a)">
    <path d="M100 40H150"/><path d="M260 40H310"/><path d="M365 62V82H215V62"/>
  </g>
  <rect x="4" y="20" width="96" height="40" rx="8" style="fill:var(--card);stroke:var(--border)"/>
  <rect x="152" y="20" width="108" height="40" rx="8" style="fill:var(--card);stroke:var(--acc);stroke-width:2"/>
  <rect x="312" y="20" width="106" height="40" rx="8" style="fill:var(--card);stroke:var(--border)"/>
  <g style="fill:var(--fg)" text-anchor="middle">
    <text x="52" y="44">Request</text><text x="206" y="44">Queue</text><text x="365" y="44">Worker</text>
  </g>
  <text x="290" y="100" text-anchor="middle" style="fill:var(--fg3);font-size:11px">retry on failure, up to 3 times</text>
</svg>
```

## Before and after
```html
<table style="width:100%;border-collapse:collapse;font:13px var(--sans);color:var(--fg)">
  <tr style="color:var(--fg3);text-align:left"><th style="padding:4px 6px"></th><th style="padding:4px 6px">Before</th><th style="padding:4px 6px">After</th></tr>
  <tr style="border-top:1px solid var(--border)"><td style="padding:4px 6px;color:var(--fg2)">p95 latency</td><td style="padding:4px 6px">840 ms</td><td style="padding:4px 6px;color:var(--ok)">210 ms</td></tr>
  <tr style="border-top:1px solid var(--border)"><td style="padding:4px 6px;color:var(--fg2)">Queries per page</td><td style="padding:4px 6px">41</td><td style="padding:4px 6px;color:var(--ok)">3</td></tr>
  <tr style="border-top:1px solid var(--border)"><td style="padding:4px 6px;color:var(--fg2)">Memory</td><td style="padding:4px 6px">180 MB</td><td style="padding:4px 6px;color:var(--warn)">195 MB</td></tr>
</table>
```

## A timeline
```html
<div style="font:13px var(--sans);color:var(--fg);border-left:2px solid var(--border);margin-left:6px;padding-left:14px">
  <div style="position:relative;margin:0 0 10px"><span style="position:absolute;left:-20px;top:5px;width:10px;height:10px;border-radius:50%;background:var(--ok)"></span><b>Mon</b> <span style="color:var(--fg2)">Schema migrated</span></div>
  <div style="position:relative;margin:0 0 10px"><span style="position:absolute;left:-20px;top:5px;width:10px;height:10px;border-radius:50%;background:var(--ok)"></span><b>Tue</b> <span style="color:var(--fg2)">Backfill finished, 1.2 M rows</span></div>
  <div style="position:relative;margin:0 0 10px"><span style="position:absolute;left:-20px;top:5px;width:10px;height:10px;border-radius:50%;background:var(--acc)"></span><b>Wed</b> <span style="color:var(--fg2)">Reads switched over (today)</span></div>
  <div style="position:relative"><span style="position:absolute;left:-20px;top:5px;width:10px;height:10px;border-radius:50%;border:2px solid var(--border);box-sizing:border-box;background:var(--bg)"></span><b>Fri</b> <span style="color:var(--fg3)">Old table dropped</span></div>
</div>
```

## A plan's steps, with their states
```html
<ol style="list-style:none;margin:0;padding:0;font:13px var(--sans);color:var(--fg)">
  <li style="display:flex;gap:8px;padding:4px 0"><span style="color:var(--ok)">✓</span><span>Write the failing test</span></li>
  <li style="display:flex;gap:8px;padding:4px 0"><span style="color:var(--ok)">✓</span><span>Fix the parser</span></li>
  <li style="display:flex;gap:8px;padding:4px 0"><span style="color:var(--acc)">●</span><b>Run the full suite</b><span style="color:var(--fg3)">in progress</span></li>
  <li style="display:flex;gap:8px;padding:4px 0"><span style="color:var(--warn)">!</span><span>Update the docs</span><span style="color:var(--warn)">needs a decision</span></li>
  <li style="display:flex;gap:8px;padding:4px 0;color:var(--fg3)"><span>○</span><span>Open the PR</span></li>
</ol>
```

## A status table
```html
<table style="width:100%;border-collapse:collapse;font:13px var(--sans);color:var(--fg)">
  <tr style="color:var(--fg3);text-align:left"><th>Step</th><th>Owner</th><th>Status</th></tr>
  <tr style="border-top:1px solid var(--border)"><td>Migrate</td><td>billing-fix</td><td style="color:var(--ok)">done</td></tr>
  <tr style="border-top:1px solid var(--border)"><td>Backfill</td><td>invoice-sync</td><td style="color:var(--warn)">waiting</td></tr>
</table>
```

## A bar chart
```html
<div style="font:13px var(--sans);color:var(--fg)">
  <div style="display:grid;grid-template-columns:90px 1fr 30px;gap:8px;align-items:center;margin:4px 0">
    <span>Done</span><div style="background:var(--okb);border-radius:4px"><div style="width:70%;height:10px;background:var(--ok);border-radius:4px"></div></div><span>7</span>
  </div>
  <div style="display:grid;grid-template-columns:90px 1fr 30px;gap:8px;align-items:center;margin:4px 0">
    <span>Blocked</span><div style="background:var(--badb);border-radius:4px"><div style="width:20%;height:10px;background:var(--bad);border-radius:4px"></div></div><span>2</span>
  </div>
</div>
```

## A sparkline
```html
<svg viewBox="0 0 200 40" width="320" style="max-width:100%">
  <polyline points="0,30 25,26 50,28 75,18 100,20 125,12 150,14 175,8 200,10" style="fill:none;stroke:var(--acc);stroke-width:2"/>
  <circle cx="200" cy="10" r="3" style="fill:var(--acc)"/>
</svg>
```
