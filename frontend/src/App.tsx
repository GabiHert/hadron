import { useCallback, useEffect, useRef, useState } from "react";
import { Board } from "./components/Board";
import { CardPanel } from "./components/CardPanel";
import type { CardTab } from "./components/CardPanel";
import { FeedbackModal } from "./components/FeedbackModal";
import { Header } from "./components/Header";
import { NewSession, NewWorkspace, Settings } from "./components/Modals";
import { CheatSheet, ConfirmDialog, Palette, paletteItems } from "./components/KeyDialogs";
import type { ConfirmReq } from "./components/KeyDialogs";
import { WhatsNew } from "./components/WhatsNew";
import { PlannerPanel } from "./components/PlannerPanel";
import { Icon } from "./components/ui";
import { useUpdateAvailable } from "./components/UpdatesCard";
import * as api from "./api";
import { releaseHighlights } from "./lib";
import { useBoard } from "./state/useBoard";
import { useKeyBindings } from "./state/useKeys";
import { useKeyboard, useLastPanel } from "./state/useKeyboard";
import { useZoom } from "./state/useZoom";
import { cardEl, focusEl, whenReady } from "./commands";
import type { Env, ModalName } from "./commands";
import { startThemes, themeActions } from "./theme";

type Modal = null | ModalName;

/** True when the window is narrow enough (960px, as in app.css) for the panels to be overlays. */
function isNarrowWindow(): boolean {
  return typeof window.matchMedia === "function" && window.matchMedia("(max-width: 960px)").matches;
}

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
  useEffect(() => startThemes(), []);
  const openFeedback = () => {
    const nick = board.selected?.nick;
    setFeedbackArea(modal === "settings" ? `settings:${settingsTabNow.current}` : ws?.ui.main_view === "dashboard" ? "dashboard" : "board" + (nick ? `:card:${nick}` : ""));
    setModal("feedback");
  };

  const { overrides } = useKeyBindings(actions.toast);
  const { zoom, step: zoomStep } = useZoom(actions.toast);
  const [confirmReq, setConfirmReq] = useState<ConfirmReq | null>(null);
  const [newIn, setNewIn] = useState<string | undefined>(undefined);
  const lastPanel = useLastPanel();
  const openSettings = (t = "columns") => {
    setSettingsTab(t);
    setModal("settings");
  };
  const env: Env = {
    board,
    modal,
    setModal,
    openSettings,
    openFeedback,
    newSessionIn: (columnId) => {
      setNewIn(columnId);
      setModal("newsession");
    },
    plannerView,
    setPlannerView,
    cardView,
    setCardView,
    setTab,
    confirm: (text, ok, run) => setConfirmReq({ text, ok, run }),
    zoom: zoomStep,
    lastPanel: () => lastPanel.current,
    toggleTheme: () => void themeActions.toggleMode(),
  };
  // The palette's "Go to card" rows: select the card, show it and put the focus on it.
  const run = useKeyboard(env, overrides, (id) => {
    if (!id.startsWith("goto:")) return undefined;
    const cid = id.slice(5);
    actions.open(cid);
    if (ws?.ui.main_view === "dashboard") void actions.setMainView("board");
    whenReady(() => cardEl(cid), (el) => focusEl(el));
    return true;
  });

  useEffect(() => {
    if (!toast) return;
    const t = setTimeout(() => actions.clearToast(), 3200);
    return () => clearTimeout(t);
  }, [toast, actions]);

  // A rule's notify action shows as a toast.
  useEffect(() => api.onNotify((e) => actions.toast(e.body ? `${e.title}: ${e.body}` : e.title)), [actions]);

  // Picking a card opens the card overlay, but only on a narrow window, where the panels are
  // overlays. On a wide window "open" would also keep a collapsed card panel from collapsing.
  const selectedId = board.selected?.id;
  useEffect(() => {
    if (selectedId && isNarrowWindow()) setMobile("card");
  }, [selectedId]);

  return (
    <div className="win" style={zoom !== 1 ? { zoom } : undefined}>
      <Header
        board={board}
        onNewWorkspace={() => setModal("newws")}
        onPalette={() => setModal("palette")}
        onFeedback={openFeedback}
        onSettings={() => openSettings()}
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
      {modal === "newsession" ? <NewSession board={board} columnId={newIn} onClose={() => { setModal(null); setNewIn(undefined); }} /> : null}
      {modal === "settings" ? <Settings board={board} initialTab={settingsTab} onTab={trackTab} onWhatsNew={() => setModal("whatsnew")} onClose={() => setModal(null)} /> : null}
      {modal === "palette" ? <Palette items={paletteItems(board, overrides, (id) => void run(id), openSettings)} onClose={() => setModal(null)} /> : null}
      {modal === "keys" ? <CheatSheet overrides={overrides} onClose={() => setModal(null)} onSettings={() => openSettings("keyboard")} /> : null}
      {confirmReq ? <ConfirmDialog req={confirmReq} onClose={() => setConfirmReq(null)} /> : null}
      {modal === "whatsnew" ? <WhatsNew onClose={() => setModal(null)} /> : null}
      {modal === "feedback" ? <FeedbackModal board={board} area={feedbackArea} onClose={() => setModal(null)} /> : null}
      {toast ? <div className="toast" role="status">{toast.text}</div> : null}
    </div>
  );
}
