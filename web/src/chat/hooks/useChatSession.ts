import { useCallback, useEffect, useRef } from 'react';
import type { Dispatch, MutableRefObject } from 'react';
import { deleteSession, loadDefaultPersona, loadSessionApprovals, loadSessionDetail, loadSessions } from '../protocol/sessionApi';
import { loadMemoryStatus } from '../protocol/memoryApi';
import type { ChatAction, ChatState } from '../state/chatTypes';

export type ChatSessionControls = {
  refreshSessions: (personaKey?: string) => Promise<void>;
  refreshApprovals: (sessionID?: string) => Promise<void>;
  refreshMemoryStatus: (sessionID?: string) => Promise<void>;
  reloadSessionHistory: (sessionID?: string) => Promise<void>;
  startNewChat: () => Promise<void>;
  switchSession: (sessionID: string, personaKey: string) => Promise<void>;
  removeSession: (sessionID: string) => Promise<void>;
};

type ChatContextRef = MutableRefObject<{ personaKey: string; sessionID: string }>;

type UseChatSessionOptions = {
  state: ChatState;
  dispatch: Dispatch<ChatAction>;
  contextRef: ChatContextRef;
  closeSocketRef: MutableRefObject<() => Promise<void>>;
  sendCommandRef: MutableRefObject<(command: string) => Promise<void>>;
  setSidebarOpen: (open: boolean) => void;
};

const DEFAULT_ORIGIN_KEY = 'webui:local:main';

export function useChatSession({ state, dispatch, contextRef, closeSocketRef, sendCommandRef, setSidebarOpen }: UseChatSessionOptions): ChatSessionControls {
  const historyRequestRef = useRef(0);
  const approvalsRequestRef = useRef(0);
  const memoryRequestRef = useRef(0);
  const sessionsRequestRef = useRef(0);
  const switchRequestRef = useRef(0);

  const refreshMemoryStatus = useCallback(async (sessionID = contextRef.current.sessionID) => {
    if (!sessionID) {
      memoryRequestRef.current += 1;
      dispatch({ type: 'SET_MEMORY_STATUS', segments: [], jobs: [] });
      return;
    }
    const requestID = ++memoryRequestRef.current;
    const targetSessionID = sessionID;
    try {
      const status = await loadMemoryStatus(targetSessionID);
      if (memoryRequestRef.current !== requestID) return;
      if (contextRef.current.sessionID !== targetSessionID) return;
      dispatch({ type: 'SET_MEMORY_STATUS', segments: status.segments, jobs: status.jobs });
    } catch {
      // Memory status is diagnostic UI; chat should continue if it is unavailable.
    }
  }, [contextRef, dispatch]);

  const refreshApprovals = useCallback(async (sessionID = contextRef.current.sessionID) => {
    if (!sessionID) {
      approvalsRequestRef.current += 1;
      dispatch({ type: 'SET_APPROVALS', approvals: [] });
      return;
    }
    const requestID = ++approvalsRequestRef.current;
    const targetSessionID = sessionID;
    try {
      const approvals = await loadSessionApprovals(targetSessionID);
      if (approvalsRequestRef.current !== requestID) return;
      if (contextRef.current.sessionID !== targetSessionID) return;
      dispatch({ type: 'SET_APPROVALS', approvals });
    } catch {
      if (approvalsRequestRef.current !== requestID) return;
      if (contextRef.current.sessionID !== targetSessionID) return;
      dispatch({ type: 'SET_APPROVALS', approvals: [] });
    }
  }, [contextRef, dispatch]);

  const refreshSessions = useCallback(async (personaKey = contextRef.current.personaKey) => {
    if (!personaKey) {
      sessionsRequestRef.current += 1;
      dispatch({ type: 'SET_SESSIONS', sessions: [] });
      return;
    }
    const requestID = ++sessionsRequestRef.current;
    const targetPersona = personaKey;
    try {
      const sessions = await loadSessions(targetPersona);
      if (sessionsRequestRef.current !== requestID) return;
      if (contextRef.current.personaKey !== targetPersona) return;
      dispatch({ type: 'SET_SESSIONS', sessions });
    } catch (error) {
      if (sessionsRequestRef.current !== requestID) return;
      if (contextRef.current.personaKey !== targetPersona) return;
      dispatch({ type: 'SET_SESSIONS', sessions: [] });
      dispatch({ type: 'SET_STATUS', status: error instanceof Error ? error.message : '会话加载失败' });
    }
  }, [contextRef, dispatch]);

  const reloadSessionHistory = useCallback(async (sessionID = contextRef.current.sessionID) => {
    if (!sessionID) return;
    const requestID = ++historyRequestRef.current;
    const targetSessionID = sessionID;
    try {
      const detail = await loadSessionDetail(targetSessionID, DEFAULT_ORIGIN_KEY);
      if (historyRequestRef.current !== requestID) return;
      if (contextRef.current.sessionID !== targetSessionID) return;
      dispatch({ type: 'SET_HISTORY', messages: detail.messages || detail.Messages || [], events: detail.events || detail.Events || [] });
      dispatch({ type: 'SET_CONTEXT_STATS', stats: sessionDetailContextStats(detail) });
    } catch (error) {
      if (historyRequestRef.current !== requestID) return;
      if (contextRef.current.sessionID !== targetSessionID) return;
      throw error;
    }
  }, [contextRef, dispatch]);

  useEffect(() => {
    let cancelled = false;
    async function bootstrapChat() {
      dispatch({ type: 'SET_STATUS', status: '加载中...' });
      const params = new URLSearchParams(location.search);
      let personaKey = params.get('persona') || '';
      let sessionID = params.get('session_id') || '';
      if (sessionID) {
        try {
          const detail = await loadSessionDetail(sessionID, DEFAULT_ORIGIN_KEY);
          personaKey = detail.persona || detail.Persona || personaKey;
          if (!cancelled) {
            dispatch({ type: 'SET_CONTEXT', sessionID, personaKey, contextStats: sessionDetailContextStats(detail) });
            contextRef.current = { sessionID, personaKey };
            dispatch({ type: 'SET_HISTORY', messages: detail.messages || detail.Messages || [], events: detail.events || detail.Events || [] });
            await Promise.all([refreshApprovals(sessionID), refreshMemoryStatus(sessionID)]);
          }
        } catch {
          sessionID = '';
        }
      }
      if (!sessionID) {
        personaKey = personaKey || await loadDefaultPersona();
        if (!cancelled) {
          dispatch({ type: 'SET_CONTEXT', sessionID: '', personaKey });
          contextRef.current = { sessionID: '', personaKey };
          dispatch({ type: 'CLEAR_TIMELINE' });
          await refreshMemoryStatus('');
        }
      }
      if (!cancelled) {
        await refreshSessions(personaKey);
        dispatch({ type: 'SET_STATUS', status: '就绪' });
      }
    }
    bootstrapChat().catch(error => {
      dispatch({ type: 'SET_STATUS', status: error instanceof Error ? error.message : '聊天初始化失败' });
      dispatch({ type: 'ADD_MESSAGE', role: 'error', content: error instanceof Error ? error.message : '聊天初始化失败' });
    });
    return () => {
      cancelled = true;
    };
  }, [contextRef, dispatch, refreshApprovals, refreshMemoryStatus, refreshSessions]);

  const startNewChat = useCallback(async () => {
    try {
      await sendCommandRef.current('/new');
      setSidebarOpen(false);
    } catch (error) {
      await closeSocketRef.current();
      historyRequestRef.current += 1;
      approvalsRequestRef.current += 1;
      memoryRequestRef.current += 1;
      dispatch({ type: 'SET_CONTEXT', sessionID: '' });
      contextRef.current = { ...contextRef.current, sessionID: '' };
      dispatch({ type: 'CLEAR_TIMELINE' });
      dispatch({ type: 'SET_MEMORY_STATUS', segments: [], jobs: [] });
      dispatch({ type: 'SET_STATUS', status: error instanceof Error ? error.message : '新会话创建失败' });
      await refreshSessions();
    }
  }, [closeSocketRef, contextRef, dispatch, refreshSessions, sendCommandRef, setSidebarOpen]);

  const switchSession = useCallback(async (sessionID: string, personaKey: string) => {
    const requestID = ++switchRequestRef.current;
    dispatch({ type: 'SET_STATUS', status: '正在加载会话...' });
    try {
      await sendCommandRef.current(`/switch ${sessionID}`);
      if (switchRequestRef.current !== requestID) return;
      setSidebarOpen(false);
    } catch (error) {
      const detail = await loadSessionDetail(sessionID, DEFAULT_ORIGIN_KEY);
      if (switchRequestRef.current !== requestID) return;
      await closeSocketRef.current();
      if (switchRequestRef.current !== requestID) return;
      const nextPersona = detail.persona || detail.Persona || personaKey || contextRef.current.personaKey;
      dispatch({ type: 'SET_CONTEXT', sessionID, personaKey: nextPersona, contextStats: sessionDetailContextStats(detail) });
      contextRef.current = { sessionID, personaKey: nextPersona };
      await reloadSessionHistory(sessionID);
      if (switchRequestRef.current !== requestID) return;
      await Promise.all([refreshApprovals(sessionID), refreshMemoryStatus(sessionID), refreshSessions(nextPersona)]);
      if (switchRequestRef.current !== requestID) return;
      dispatch({ type: 'SET_STATUS', status: error instanceof Error ? error.message : '会话加载失败' });
    }
  }, [closeSocketRef, contextRef, dispatch, refreshApprovals, refreshMemoryStatus, refreshSessions, reloadSessionHistory, sendCommandRef, setSidebarOpen]);

  const removeSession = useCallback(async (sessionID: string) => {
    if (!sessionID) return;
    try {
      await deleteSession(sessionID);
      if (sessionID === contextRef.current.sessionID) await startNewChat();
      else await refreshSessions();
    } catch (error) {
      dispatch({ type: 'SET_STATUS', status: error instanceof Error ? error.message : '会话删除失败' });
    }
  }, [contextRef, dispatch, refreshSessions, startNewChat]);

  return { refreshSessions, refreshApprovals, refreshMemoryStatus, reloadSessionHistory, startNewChat, switchSession, removeSession };
}

function sessionDetailContextStats(detail: Awaited<ReturnType<typeof loadSessionDetail>>) {
  return detail.context_stats || detail.contextStats || detail.ContextStats || null;
}
