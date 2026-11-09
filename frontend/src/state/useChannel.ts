import { useCallback, useEffect, useRef, useState } from "react";
import * as api from "../api";
import type { Message, Question } from "../api";

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e));

export interface Channel {
  messages: Message[];
  /** the card's questions, every status; empty for the planner channel */
  questions: Question[];
  loaded: boolean;
  send: (text: string) => Promise<void>;
  answer: (questionId: string, selections: string[], notes: string) => Promise<void>;
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
  const seq = useRef(0);
  const errRef = useRef(onError);
  errRef.current = onError;

  const load = useCallback(async () => {
    if (!workspaceId) return;
    const mine = ++seq.current;
    try {
      const [msgs, qs] = await Promise.all([
        api.chat_read({ workspace_id: workspaceId, channel }),
        cardId ? api.question_list({ workspace_id: workspaceId, card: cardId }) : Promise.resolve([] as Question[]),
      ]);
      if (mine !== seq.current) return;
      setMessages(msgs);
      setQuestions(qs);
      setLoaded(true);
    } catch (e) {
      if (mine === seq.current) errRef.current(errText(e));
    }
  }, [workspaceId, channel, cardId]);

  useEffect(() => {
    setMessages([]);
    setQuestions([]);
    setLoaded(false);
    void load();
    let timer: ReturnType<typeof setTimeout> | undefined;
    const off = api.onChanged((e) => {
      if (e.entities?.length && !e.entities.some((x) => x === "messages" || x === "questions")) return;
      clearTimeout(timer);
      timer = setTimeout(() => void load(), 30);
    });
    return () => {
      seq.current++;
      clearTimeout(timer);
      off();
    };
  }, [load]);

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
  const answer = useCallback(
    (questionId: string, selections: string[], notes: string) => guard(() => api.question_answer({ question_id: questionId, selections, notes })),
    [guard],
  );
  return { messages, questions, loaded, send, answer };
}
