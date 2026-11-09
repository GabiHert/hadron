/** The Colored collision mark. The dark variant shows on dark themes, the light one on light themes (see .logo-dark and .logo-light in app.css). */
export function Logo({ size = 22 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 64 64" role="img" aria-label="Hadron">
      <g className="logo-dark">
        <g fill="none" stroke="#eef0f7" strokeWidth="5.5" strokeLinecap="round">
          <path d="M8 9C21 21 21 43 8 55" />
          <path d="M56 9C43 21 43 43 56 55" />
        </g>
        <g style={{ isolation: "isolate" }}>
          <circle cx="32" cy="27" r="7.5" fill="#5b7cfa" style={{ mixBlendMode: "screen" }} />
          <circle cx="27.4" cy="35" r="7.5" fill="#2fbf8f" style={{ mixBlendMode: "screen" }} />
          <circle cx="36.6" cy="35" r="7.5" fill="#f2a33a" style={{ mixBlendMode: "screen" }} />
        </g>
      </g>
      <g className="logo-light">
        <g fill="none" stroke="#1d2330" strokeWidth="7" strokeLinecap="round">
          <path d="M8 9C21 21 21 43 8 55" />
          <path d="M56 9C43 21 43 43 56 55" />
        </g>
        <circle cx="32" cy="27" r="8" fill="#24408e" opacity=".85" />
        <circle cx="27.4" cy="35" r="8" fill="#2f7a4b" opacity=".75" />
        <circle cx="36.6" cy="35" r="8" fill="#c07a12" opacity=".75" />
      </g>
    </svg>
  );
}

// Paths from the prototype's icon set (24x24, stroked).
const P = {
  search: "M11 4a7 7 0 1 0 0 14 7 7 0 0 0 0-14zM20 20l-3.5-3.5",
  plus: "M12 5v14M5 12h14",
  gear: "M12 9a3 3 0 1 0 0 6 3 3 0 0 0 0-6zM19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z",
  moon: "M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z",
  sun: "M12 4V2M12 22v-2M4.9 4.9 3.5 3.5M20.5 20.5l-1.4-1.4M4 12H2M22 12h-2M4.9 19.1l-1.4 1.4M20.5 3.5l-1.4 1.4M12 7a5 5 0 1 0 0 10 5 5 0 0 0 0-10z",
  dots: "M5 12h.01M12 12h.01M19 12h.01",
  close: "M6 6l12 12M18 6 6 18",
  chat: "M4 5h16v11H9l-5 4z",
  bubble: "M4 5h16v11H10l-4 4v-4H4z",
  term: "M4 5h16v14H4zM8 10l3 2-3 2M13 15h3",
  down: "M12 4v11m0 0-4-4m4 4 4-4M5 20h14",
  link: "M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1",
  back: "M15 6l-6 6 6 6",
  fwd: "M9 6l6 6-6 6",
  board: "M4 4h5v16H4zM10 4h5v10h-5zM16 4h4v13h-4z",
  expand: "M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5",
  shrink: "M9 4v5H4M15 4v5h5M9 20v-5H4M15 20v-5h5",
  dash: "M4 4h7v7H4zM13 4h7v4h-7zM13 10h7v10h-7zM4 13h7v7H4z",
} as const;

export type IconName = keyof typeof P;

export function Icon({ name, size = 16 }: { name: IconName; size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d={P[name]} />
    </svg>
  );
}
