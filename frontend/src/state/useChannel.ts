import { useCallback, useEffect, useRef, useState } from "react";
import * as api from "../api";
import type { Message, Question } from "../api";

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e));

/**
 * How many of a channel's newest messages the window holds; "Show earlier messages" adds as many
 * again. A planner channel collects thousands of lines and widgets over weeks, and the whole of it
 * was read, kept and drawn on every new message.
 */
export const CHAT_PAGE = 200;

export interface Channel {
  messages: Message[];
  /** the card's questions, every status; empty for the planner channel */
  questions: Question[];
  loaded: boolean;
  send: (text: string) => Promise<void>;
  /**
   * Types a /command into the channel's session terminal (chat_command). Its error is not toasted:
   * it rejects so the composer can show it inline and offer Start.
   */
  command: (text: string) => Promise<void>;
  answer: (questionId: string, selections: string[], notes: string) => Promise<void>;
  /** older messages exist beyond the ones held */
  hasEarlier: boolean;
  /** holds CHAT_PAGE more of the older messages */
  showEarlier: () => void;
}

/**
 * A chat channel: its messages and, for a card, its questions. It loads on mount and again on every
 * `changed` event that touches messages or questions, so a reply the planner posts appears at once.
 * Errors from send and answer go to onError, then rethrow so the composer keeps what was typed.
 */
export function useChannel(workspaceId: string, channel: string, cardId: string | null, onError: (text: string) => void): Channel {
  const [messages, setMessages] = useState<Message[]>([]);
  const [questions, setQuestions] = useState<Question[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [limit, setLimit] = useState(CHAT_PAGE);
  const seq = useRef(0);
  const errRef = useRef(onError);
  errRef.current = onError;

  const load = useCallback(async () => {
    if (!workspaceId) return;
    const mine = ++seq.current;
    try {
      const [msgs, qs] = await Promise.all([
        api.chat_read({ workspace_id: workspaceId, channel, limit }),
        cardId ? api.question_list({ workspace_id: workspaceId, card: cardId }) : Promise.resolve([] as Question[]),
      ]);
      if (mine !== seq.current) return;
      setMessages(msgs);
      setQuestions(qs);
      setLoaded(true);
    } catch (e) {
      if (mine === seq.current) errRef.current(errText(e));
    }
  }, [workspaceId, channel, cardId, limit]);

  // Another conversation starts empty, from its newest page. (Showing earlier messages only
  // reloads: the lines on screen stay while the older ones arrive.)
  useEffect(() => {
    setLimit(CHAT_PAGE);
    setMessages([]);
    setQuestions([]);
    setLoaded(false);
  }, [workspaceId, channel]);

  useEffect(() => {
    void load();
    let timer: ReturnType<typeof setTimeout> | undefined;
    const off = api.onChanged((e) => {
      if (e.entities?.length && !e.entities.some((x) => x === "messages" || x === "questions")) return;
      // A message in another workspace is not in this channel.
      if (e.workspace_id && e.workspace_id !== workspaceId) return;
      clearTimeout(timer);
      timer = setTimeout(() => void load(), 30);
    });
    return () => {
      seq.current++;
      clearTimeout(timer);
      off();
    };
  }, [load, workspaceId]);

  const guard = useCallback(
    async (fn: () => Promise<unknown>) => {
      try {
        await fn();
      } catch (e) {
        errRef.current(errText(e));
        throw e;
      } finally {
        await load();
      }
    },
    [load],
  );

  const send = useCallback((text: string) => guard(() => api.chat_send({ workspace_id: workspaceId, channel, text })), [guard, workspaceId, channel]);
  const command = useCallback(
    async (text: string) => {
      try {
        await api.chat_command({ workspace_id: workspaceId, channel, text });
      } finally {
        await load();
      }
    },
    [load, workspaceId, channel],
  );
  const answer = useCallback(
    (questionId: string, selections: string[], notes: string) => guard(() => api.question_answer({ question_id: questionId, selections, notes })),
    [guard],
  );
  const showEarlier = useCallback(() => setLimit((n) => n + CHAT_PAGE), []);
  return { messages, questions, loaded, send, command, answer, hasEarlier: messages.length >= limit, showEarlier };
}
