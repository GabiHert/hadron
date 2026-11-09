import { useEffect, useState } from "react";
import { Board } from "./components/Board";
import { CardPanel } from "./components/CardPanel";
import type { CardTab } from "./components/CardPanel";
import { Header } from "./components/Header";
import { NewSession, NewWorkspace, Palette, Settings } from "./components/Modals";
import { PlannerPanel } from "./components/PlannerPanel";
import { Icon } from "./components/ui";
import { useUpdateAvailable } from "./components/UpdatesCard";
import * as api from "./api";
import { useBoard } from "./state/useBoard";

type Modal = null | "newws" | "newsession" | "settings" | "palette";

export default function App() {
  const board = useBoard();
  const { ws, toast, actions } = board;
  const [modal, setModal] = useState<Modal>(null);
  const [plannerView, setPlannerView] = useState<"chat" | "terminal">("chat");
  const [cardView, setCardView] = useState<"chat" | "terminal">("chat");
  const [tab, setTab] = useState<CardTab>("conv");
  const [mobile, setMobile] = useState<null | "planner" | "card">(null);
  const [settingsTab, setSettingsTab] = useState("columns");
  const update = useUpdateAvailable();

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
        onSettings={() => {
          setSettingsTab("columns");
          setModal("settings");
        }}
        updateSlot={
          update?.latest ? (
            <button
              className="pill acc hide-narrow"
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
      {modal === "settings" ? <Settings board={board} initialTab={settingsTab} onClose={() => setModal(null)} /> : null}
      {modal === "palette" ? (
        <Palette board={board} onClose={() => setModal(null)} openModal={(m) => setModal(m)} setPlannerView={setPlannerView} />
      ) : null}
      {toast ? <div className="toast" role="status">{toast.text}</div> : null}
    </div>
  );
}
