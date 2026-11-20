import { useCallback, useEffect, useRef, useState } from "react";
import { Board, ViewSeg } from "./components/Board";
import { SessionsSpace } from "./components/SessionsSpace";
import { NewFreeSession } from "./components/NewFreeSession";
import { CardPanel } from "./components/CardPanel";
import type { CardTab } from "./components/CardPanel";
import { FeedbackModal } from "./components/FeedbackModal";
import { Header } from "./components/Header";
import { NewSession, NewWorkspace, Settings } from "./components/Modals";
import { CheatSheet, ConfirmDialog, Palette, paletteItems } from "./components/KeyDialogs";
import type { ConfirmReq } from "./components/KeyDialogs";
import { WhatsNew } from "./components/WhatsNew";
import { ambientOff, ambientOn, uiSound } from "./tourAudio";
import { chapterStart, SETTING_TUTORIAL_AMBIENT, SETTING_TUTORIAL_MUTED, SETTING_TUTORIAL_SEEN, TUTORIAL_EVENT, Tutorial } from "./components/Tutorial";
import type { McpGlance, TourSound } from "./components/Tutorial";
import { fillComposer } from "./components/Composer";
import { QuitGuard } from "./components/QuitGuard";
import { PlannerPanel } from "./components/PlannerPanel";
import { Icon } from "./components/ui";
import { useUpdateAvailable } from "./components/UpdatesCard";
import * as api from "./api";
import { releaseHighlights } from "./lib";
import { useBoard } from "./state/useBoard";
import { useKeyBindings } from "./state/useKeys";
import { useKeyboard, useLastPanel } from "./state/useKeyboard";
import { useZoom } from "./state/useZoom";
import { useGlass } from "./state/useGlass";
import { cardEl, focusEl, whenReady } from "./commands";
import type { Env, ModalName } from "./commands";
import { startThemes, themeActions } from "./theme";
import { OVERLAY_MAX, usePanelLayout } from "./state/panelLayout";

type Modal = null | ModalName;

/** True when the window is narrow enough (OVERLAY_MAX, as in app.css) for the panels to be overlays. */
function isNarrowWindow(): boolean {
  return typeof window.matchMedia === "function" && window.matchMedia(`(max-width: ${OVERLAY_MAX}px)`).matches;
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
  const wsRef = useRef(ws);
  wsRef.current = ws;
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
  // Once, when the board has loaded: the tutorial on a first launch (it was never seen), which
  // also holds What's new back until the next launch.
  const [firstRun, setFirstRun] = useState<"pending" | "tour" | "done">("pending");
  useEffect(() => {
    if (!board.ready || firstRun !== "pending") return;
    Promise.resolve(api.settings_get())
      .then((s) => {
        const seen = (s as Record<string, unknown> | null)?.[SETTING_TUTORIAL_SEEN] === true;
        setFirstRun(seen ? "done" : "tour");
        if (!seen) {
          setTourStart(0);
          setModal((m) => m ?? "tutorial");
        }
      })
      .catch(() => setFirstRun("done"));
  }, [board.ready, firstRun]);
  // The tutorial starts at a chapter when asked from Settings, ⌘K or tutorial_show.
  const [tourStart, setTourStart] = useState(0);
  const [tourMcp, setTourMcp] = useState<McpGlance | null>(null);
  const openTutorial = useCallback((chapter?: string) => {
    setTourStart(chapterStart(chapter));
    setModal("tutorial");
  }, []);
  useEffect(() => api.onEvent("tutorial:show", (d) => openTutorial((d as { chapter?: string } | null)?.chapter)), [openTutorial]);
  useEffect(() => {
    const on = (e: Event) => openTutorial((e as CustomEvent<{ chapter?: string }>).detail?.chapter);
    window.addEventListener(TUTORIAL_EVENT, on);
    return () => window.removeEventListener(TUTORIAL_EVENT, on);
  }, [openTutorial]);
  // The MCP steps show the real server's address and tool count (never its token).
  useEffect(() => {
    if (modal !== "tutorial") return;
    Promise.resolve(api.mcp_info())
      .then((i) => i && setTourMcp({ url: i.url, tools: i.tool_count }))
      .catch(() => undefined);
  }, [modal]);
  // The tutorial's sounds go through Hadron's player (silent when Sounds are off); its own mute
  // toggle is remembered in tutorial.muted.
  const [tourMuted, setTourMuted] = useState(false);
  // The Sounds volume (0 when Sounds are off), for the sounds the window makes itself: the
  // caption's clicks and the ambient pad. The ambient pad has its own switch, on by default.
  const [tourVol, setTourVol] = useState(0);
  const [tourAmbient, setTourAmbient] = useState(true);
  const ambientTour = useCallback((on: boolean) => {
    setTourAmbient(on);
    void Promise.resolve(api.settings_update({ values: { [SETTING_TUTORIAL_AMBIENT]: on } })).catch(() => undefined);
  }, []);
  const uiTour = useCallback((kind: "hover" | "press") => uiSound(kind, tourVol), [tourVol]);
  useEffect(() => {
    if (modal === "tutorial" && !tourMuted && tourAmbient && tourVol > 0) ambientOn(tourVol);
    else ambientOff();
  }, [modal, tourMuted, tourAmbient, tourVol]);
  useEffect(() => () => ambientOff(), []);
  const playTour = useCallback((sound: TourSound) => void Promise.resolve(api.sound_play({ sound })).catch(() => undefined), []);
  const muteTour = useCallback((m: boolean) => {
    setTourMuted(m);
    void Promise.resolve(api.settings_update({ values: { [SETTING_TUTORIAL_MUTED]: m } })).catch(() => undefined);
  }, []);
  useEffect(() => {
    if (modal !== "tutorial") return;
    Promise.resolve(api.settings_get())
      .then((raw) => {
        const s = (raw ?? {}) as Record<string, unknown>;
        setTourMuted(s[SETTING_TUTORIAL_MUTED] === true);
        setTourAmbient(s[SETTING_TUTORIAL_AMBIENT] !== false);
        const vol = typeof s["sounds.volume"] === "number" ? (s["sounds.volume"] as number) : 0.6;
        setTourVol(s["sounds.enabled"] === false ? 0 : vol);
      })
      .catch(() => undefined);
  }, [modal]);
  const endTutorial = useCallback((_finished: boolean, ask?: string) => {
    setModal((m) => (m === "tutorial" ? null : m));
    void Promise.resolve(api.settings_update({ values: { [SETTING_TUTORIAL_SEEN]: true } })).catch(() => undefined);
    if (!ask) return;
    // The last step hands over to the planner: its message box gets the prompt, editable, not sent.
    fillComposer("planner-msg", ask);
    const cur = wsRef.current;
    if (!cur) {
      setModal("newws");
      actions.toast("Create a workspace first; your message to the planner will be waiting there.");
      return;
    }
    setPlannerView("chat");
    if (cur.ui.full && cur.ui.full !== "planner") void actions.setFullscreen("");
    if (cur.ui.collapsed?.planner) void actions.setCollapsed("planner", false);
    if (isNarrowWindow()) setMobile("planner");
  }, [actions]);
  // Once after the board loads: open What's new when a release nobody has seen is running.
  useEffect(() => {
    if (!ws || checkedChangelog.current || firstRun !== "done") return;
    checkedChangelog.current = true;
    Promise.resolve(api.changelog_get()).then((i) => { if (i?.show) setModal((m) => m ?? "whatsnew"); }).catch(() => undefined);
  }, [ws, firstRun]);
  useEffect(() => api.onEvent("menu:whatsnew", () => setModal("whatsnew")), []);
  useEffect(() => startThemes(), []);
  const openFeedback = () => {
    const nick = board.selected?.nick;
    setFeedbackArea(modal === "settings" ? `settings:${settingsTabNow.current}` : ws?.ui.main_view === "dashboard" ? "dashboard" : ws?.ui.main_view === "sessions" ? "sessions" : "board" + (nick ? `:card:${nick}` : ""));
    setModal("feedback");
  };

  const { overrides } = useKeyBindings(actions.toast);
  const { zoom, step: zoomStep } = useZoom(actions.toast);
  useGlass();
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
    if (ws?.ui.main_view !== "board") void actions.setMainView("board");
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

  // Go skips notifications about the card the user is looking at, and planner replies while the
  // window has the focus, so it needs to know both.
  const presenceWs = ws?.id ?? "";
  const presenceCard = ws && !ws.ui.collapsed?.card ? (board.selected?.id ?? "") : "";
  useEffect(() => {
    const report = () => {
      const focused = document.hasFocus() && document.visibilityState !== "hidden";
      void api.ui_presence({ focused, workspace_id: presenceWs, card_id: presenceCard }).catch(() => {});
    };
    report();
    window.addEventListener("focus", report);
    window.addEventListener("blur", report);
    document.addEventListener("visibilitychange", report);
    return () => {
      window.removeEventListener("focus", report);
      window.removeEventListener("blur", report);
      document.removeEventListener("visibilitychange", report);
    };
  }, [presenceWs, presenceCard]);

  // A click on a macOS notification: show its workspace, then the card or the planner chat.
  const runRef = useRef(run);
  runRef.current = run;
  const activeRef = useRef(ws?.id);
  activeRef.current = ws?.id;
  useEffect(
    () =>
      api.onNotificationOpen((e) => {
        if (e.workspace_id && e.workspace_id !== activeRef.current) void actions.switchWorkspace(e.workspace_id);
        // Let the workspace render before opening something in it.
        setTimeout(() => {
          if (e.card_id) void runRef.current(`goto:${e.card_id}`);
          else if (e.planner) void runRef.current("focus.planner");
        }, 0);
      }),
    [actions],
  );

  // Picking a card opens the card overlay, but only on a narrow window, where the panels are
  // overlays. On a wide window "open" would also keep a collapsed card panel from collapsing.
  const selectedId = board.selected?.id;
  const panels = usePanelLayout({ zoom, collapsed: ws?.ui.collapsed ?? {}, setCollapsed: actions.setCollapsed, toast: actions.toast, selectedId });
  const fit = !panels.layout.overlay;
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
        {ws?.ui.main_view === "sessions" ? (
          // The Sessions space is a terminal app: it takes the whole width, and is the same in every workspace.
          <SessionsSpace board={board} viewSeg={<ViewSeg board={board} />} onNewIn={() => setModal("newfree")} confirm={env.confirm} />
        ) : (
          <>
            <PlannerPanel board={board} view={plannerView} setView={setPlannerView} mobileOpen={mobile === "planner"} closeMobile={() => setMobile(null)} ctl={fit ? panels.planner : undefined} zoom={zoom} />
            <Board board={board} onNewSession={() => setModal("newsession")} onNewWorkspace={() => setModal("newws")} />
            <CardPanel board={board} view={cardView} setView={setCardView} tab={tab} setTab={setTab} mobileOpen={mobile === "card"} closeMobile={() => setMobile(null)} ctl={fit ? panels.card : undefined} zoom={zoom} />
          </>
        )}
      </div>
      {modal === "newws" ? <NewWorkspace board={board} onClose={() => setModal(null)} /> : null}
      {modal === "newsession" ? <NewSession board={board} columnId={newIn} onClose={() => { setModal(null); setNewIn(undefined); }} /> : null}
      {modal === "newfree" ? <NewFreeSession board={board} onClose={() => setModal(null)} onStarted={() => { if (ws && ws.ui.main_view !== "sessions") void actions.setMainView("sessions"); }} /> : null}
      {modal === "settings" ? <Settings board={board} initialTab={settingsTab} onTab={trackTab} onWhatsNew={() => setModal("whatsnew")} onTutorial={(chapter?: string) => openTutorial(chapter)} onClose={() => setModal(null)} /> : null}
      {modal === "palette" ? <Palette items={paletteItems(board, overrides, (id) => void run(id), openSettings)} onClose={() => setModal(null)} /> : null}
      {modal === "keys" ? <CheatSheet overrides={overrides} onClose={() => setModal(null)} onSettings={() => openSettings("keyboard")} /> : null}
      {confirmReq ? <ConfirmDialog req={confirmReq} onClose={() => setConfirmReq(null)} /> : null}
      {modal === "whatsnew" ? <WhatsNew onClose={() => setModal(null)} /> : null}
      {modal === "tutorial" ? <Tutorial key={tourStart} start={tourStart} mcp={tourMcp} play={playTour} muted={tourMuted} onMute={muteTour} ui={uiTour} ambient={tourAmbient} onAmbient={ambientTour} onClose={endTutorial} /> : null}
      {modal === "feedback" ? <FeedbackModal board={board} area={feedbackArea} onClose={() => setModal(null)} /> : null}
      <QuitGuard toast={actions.toast} />
      {toast ? <div className="toast" role="status">{toast.text}</div> : null}
    </div>
  );
}
