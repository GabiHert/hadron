import * as api from "./api";

/** Forwards uncaught errors and unhandled rejections to the backend log. Returns an uninstall function. */
export function installErrorLogging(target: Window): () => void {
  let last = "";
  let lastAt = 0;
  const send = (message: string, stack?: string) => {
    try {
      const now = Date.now();
      if (message === last && now - lastAt < 2000) return;
      last = message;
      lastAt = now;
      void Promise.resolve(api.client_log({ level: "error", message, stack })).catch(() => undefined);
    } catch {
      /* never throw from a log handler */
    }
  };
  const onError = (e: Event) => {
    const ev = e as ErrorEvent;
    send(ev.message || String(ev.error ?? "error"), ev.error instanceof Error ? ev.error.stack : undefined);
  };
  const onRejection = (e: Event) => {
    const r = (e as PromiseRejectionEvent).reason;
    send(r instanceof Error ? r.message : String(r), r instanceof Error ? r.stack : undefined);
  };
  target.addEventListener("error", onError);
  target.addEventListener("unhandledrejection", onRejection);
  return () => {
    target.removeEventListener("error", onError);
    target.removeEventListener("unhandledrejection", onRejection);
  };
}
