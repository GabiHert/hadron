import { useCallback, useEffect, useRef, useState } from "react";
import { Board } from "./components/Board";
import { CardPanel } from "./components/CardPanel";
import type { CardTab } from "./components/CardPanel";
import { FeedbackModal } from "./components/FeedbackModal";
import { Header } from "./components/Header";
import { NewSession, NewWorkspace, Palette, Settings } from "./components/Modals";
import { WhatsNew } from "./components/WhatsNew";
import { PlannerPanel } from "./components/PlannerPanel";
import { Icon } from "./components/ui";
import { useUpdateAvailable } from "./components/UpdatesCard";
import * as api from "./api";
import { releaseHighlights } from "./lib";
import { useBoard } from "./state/useBoard";

type Modal = null | "newws" | "newsession" | "settings" | "palette" | "feedback" | "whatsnew";

/** The tooltip of the "<tag> available" pill: the first highlights of the release's notes. */
function updateTitle(tag: string, notes: string): string {
  const h = releaseHighlights(notes ?? "", 3);
  return h.length ? `${tag} is available. What's new: ${h.join("; ")}. Open Settings, Updates to install it.` : `${tag} is available. Open Settings, Updates to install it.`;
}

export default function App() {
  const board = useBoard();
  const { ws, toast, actions } = board;
  const [modal, setModal] = useState<Modal>(null);
  const [plannerView, setPlannerView] = useState<"chat" | "terminal">("chat");
  const [cardView, setCardView] = useState<"chat" | "terminal">("chat");
  const [tab, setTab] = useState<CardTab>("conv");
  const [mobile, setMobile] = useState<null | "planner" | "card">(null);
  const [settingsTab, setSettingsTab] = useState("columns");
  const settingsTabNow = useRef("columns");
  const trackTab = useCallback((id: string) => { settingsTabNow.current = id; }, []);
  const [feedbackArea, setFeedbackArea] = useState("board");
  const update = useUpdateAvailable();
  const checkedChangelog = useRef(false);
  // Once after the board loads: open What's new when a release nobody has seen is running.
  useEffect(() => {
    if (!ws || checkedChangelog.current) return;
    checkedChangelog.current = true;
    Promise.resolve(api.changelog_get()).then((i) => { if (i?.show) setModal((m) => m ?? "whatsnew"); }).catch(() => undefined);
  }, [ws]);
  useEffect(() => api.onEvent("menu:whatsnew", () => setModal("whatsnew")), []);
  const openFeedback = () => {
    const nick = board.selected?.nick;
    setFeedbackArea(modal === "settings" ? `settings:${settingsTabNow.current}` : ws?.ui.main_view === "dashboard" ? "dashboard" : "board" + (nick ? `:card:${nick}` : ""));
    setModal("feedback");
  };

  const full = ws?.ui.full;
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const k = e.key.toLowerCase();
      if ((e.metaKey || e.ctrlKey) && k === "k") {
        e.preventDefault();
        setModal((m) => (m === "palette" ? null : "palette"));
      } else if ((e.metaKey || e.ctrlKey) && k === "n") {
        e.preventDefault();
        setModal("newsession");
      } else if (e.key === "Escape") {
        if (modal) setModal(null);
        else if (full) void actions.setFullscreen("");
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [modal, full, actions]);

  useEffect(() => {
    if (!toast) return;
    const t = setTimeout(() => actions.clearToast(), 3200);
    return () => clearTimeout(t);
  }, [toast, actions]);

  // A rule's notify action shows as a toast.
  useEffect(() => api.onNotify((e) => actions.toast(e.body ? `${e.title}: ${e.body}` : e.title)), [actions]);

  // Picking a card on a narrow window opens the card overlay.
  const selectedId = board.selected?.id;
  useEffect(() => {
    if (selectedId) setMobile("card");
  }, [selectedId]);

  return (
    <div className="win">
      <Header
        board={board}
        onNewWorkspace={() => setModal("newws")}
        onPalette={() => setModal("palette")}
        onFeedback={openFeedback}
        onSettings={() => {
          setSettingsTab("columns");
          setModal("settings");
        }}
        updateSlot={
          update?.latest ? (
            <button
              className="pill acc hide-narrow"
              title={updateTitle(update.latest.tag, update.latest.notes)}
              onClick={() => {
                setSettingsTab("updates");
                setModal("settings");
              }}
            >
              <Icon name="down" size={12} />
              {update.latest.tag} available
            </button>
          ) : null
        }
        onTogglePlanner={() => setMobile(mobile === "planner" ? null : "planner")}
      />
      <div className="shell">
        <PlannerPanel board={board} view={plannerView} setView={setPlannerView} mobileOpen={mobile === "planner"} closeMobile={() => setMobile(null)} />
        <Board board={board} onNewSession={() => setModal("newsession")} onNewWorkspace={() => setModal("newws")} />
        <CardPanel board={board} view={cardView} setView={setCardView} tab={tab} setTab={setTab} mobileOpen={mobile === "card"} closeMobile={() => setMobile(null)} />
      </div>
      {modal === "newws" ? <NewWorkspace board={board} onClose={() => setModal(null)} /> : null}
      {modal === "newsession" ? <NewSession board={board} onClose={() => setModal(null)} /> : null}
      {modal === "settings" ? <Settings board={board} initialTab={settingsTab} onTab={trackTab} onWhatsNew={() => setModal("whatsnew")} onClose={() => setModal(null)} /> : null}
      {modal === "palette" ? (
        <Palette board={board} onClose={() => setModal(null)} openModal={(m) => (m === "feedback" ? openFeedback() : setModal(m))} setPlannerView={setPlannerView} />
      ) : null}
      {modal === "whatsnew" ? <WhatsNew onClose={() => setModal(null)} /> : null}
      {modal === "feedback" ? <FeedbackModal board={board} area={feedbackArea} onClose={() => setModal(null)} /> : null}
      {toast ? <div className="toast" role="status">{toast.text}</div> : null}
    </div>
  );
}
