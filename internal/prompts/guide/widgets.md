# Widgets

`widget_post {channel, title, html}` puts a small HTML widget in a chat. A card session posts on its own card's channel; the planner and the user on any. `widget_pin {message_id}` pins it to the dashboard.

Rules of the frame: sandboxed iframe, no network, no cookies, no access to Hadron, CSP `default-src 'none'; style-src 'unsafe-inline'; img-src data:`. Scripts are off unless the user turned them on. Keep it under 256 KB. Use the theme's CSS variables so it matches light and dark: `--bg --card --fg --fg2 --fg3 --border --acc --ok --warn --bad --okb --warnb --badb --mono --sans --serif`.

## A table
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

## A plan with a diagram (inline SVG)
```html
<svg viewBox="0 0 320 90" width="100%" style="font:12px var(--sans)">
  <rect x="4" y="25" width="90" height="40" rx="8" fill="var(--card)" stroke="var(--border)"/><text x="49" y="49" text-anchor="middle" fill="var(--fg)">Spec</text>
  <rect x="115" y="25" width="90" height="40" rx="8" fill="var(--card)" stroke="var(--acc)"/><text x="160" y="49" text-anchor="middle" fill="var(--fg)">Build</text>
  <rect x="226" y="25" width="90" height="40" rx="8" fill="var(--card)" stroke="var(--border)"/><text x="271" y="49" text-anchor="middle" fill="var(--fg)">Ship</text>
  <path d="M94 45h21M205 45h21" stroke="var(--fg3)" stroke-width="2"/>
</svg>
```
