import { useState } from "react";
import type { ReactNode } from "react";
import ReactMarkdown from "react-markdown";
import type { Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { BrowserOpenURL } from "../../wailsjs/runtime/runtime";
import { attachmentsIn } from "../attach";

/** Only these open in the browser; any other link is shown as text. */
const EXTERNAL = /^(https?:|mailto:)/i;

function openExternal(url: string) {
  try {
    BrowserOpenURL(url);
  } catch {
    // no Wails runtime (tests, a plain browser): nothing to open
  }
}

function textOf(node: ReactNode): string {
  if (node == null || typeof node === "boolean") return "";
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(textOf).join("");
  if (typeof node === "object" && "props" in node) return textOf((node as { props: { children?: ReactNode } }).props.children);
  return "";
}

/** A fenced code block with a Copy button. */
function CodeBlock({ children, lang }: { children: ReactNode; lang: string }) {
  const [copied, setCopied] = useState(false);
  const text = textOf(children).replace(/\n$/, "");
  return (
    <div className="md-pre">
      <div className="md-pre-bar">
        <span className="mono">{lang}</span>
        <button
          type="button"
          className="md-copy"
          aria-label="Copy code"
          onClick={() => {
            void navigator.clipboard?.writeText(text).then(() => {
              setCopied(true);
              setTimeout(() => setCopied(false), 1400);
            }, () => undefined);
          }}
        >
          {copied ? "Copied" : "Copy"}
        </button>
      </div>
      <pre><code>{text}</code></pre>
    </div>
  );
}

const components: Components = {
  a: ({ href, children }) => {
    const url = href ?? "";
    if (!EXTERNAL.test(url)) return <span className="md-link-off">{children}</span>;
    return (
      <a
        href={url}
        title={url}
        onClick={(e) => {
          // Never navigate the webview: the link opens in the system browser.
          e.preventDefault();
          openExternal(url);
        }}
      >
        {children}
      </a>
    );
  },
  // Remote images are never loaded; the alt text and the address stay readable.
  img: ({ alt, src }) => <span className="md-img-off" title={src ?? ""}>{alt ? `[image: ${alt}]` : "[image]"}</span>,
  pre: ({ children }) => {
    const code = Array.isArray(children) ? children[0] : children;
    const cls = (code as { props?: { className?: string } } | undefined)?.props?.className ?? "";
    return <CodeBlock lang={cls.replace(/^language-/, "")}>{(code as { props?: { children?: ReactNode } } | undefined)?.props?.children ?? children}</CodeBlock>;
  },
  code: ({ children, className }) => <code className={"md-code" + (className ? " " + className : "")}>{children}</code>,
  table: ({ children }) => (
    <div className="md-table" role="region" aria-label="Table" tabIndex={0}>
      <table>{children}</table>
    </div>
  ),
  input: ({ checked }) => <input type="checkbox" checked={!!checked} disabled readOnly aria-label={checked ? "done" : "not done"} />,
};

interface Props {
  text: string;
  className?: string;
  /** Show thumbnails of the attachments the text names (chat messages). */
  attachments?: boolean;
}

/**
 * Markdown as Hadron shows it everywhere a message, report, question or prose block is drawn: GFM
 * tables, task lists, links that open in the system browser, code with a Copy button. Raw HTML is
 * shown as text, never rendered, and remote images are never loaded.
 */
export function Markdown({ text, className = "", attachments = false }: Props) {
  const files = attachments ? attachmentsIn(text) : [];
  return (
    <div className={"md" + (className ? " " + className : "")}>
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
        {text}
      </ReactMarkdown>
      {files.length ? (
        <div className="att-row" aria-label="Attachments">
          {files.map((f) =>
            f.pdf ? (
              <span key={f.path} className="att-chip pdf" title={f.path}>PDF · {f.path.split("/").pop()}</span>
            ) : (
              <img key={f.path} className="att-thumb" src={f.url} alt={`Attached image ${f.path.split("/").pop()}`} title={f.path} loading="lazy" />
            ),
          )}
        </div>
      ) : null}
    </div>
  );
}
