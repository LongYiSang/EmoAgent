import { useCallback, useEffect } from 'react';
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
  setSidebarOpen: (open: boolean) => void;
};

export function useChatSession({ state, dispatch, contextRef, closeSocketRef, setSidebarOpen }: UseChatSessionOptions): ChatSessionControls {
  const refreshMemoryStatus = useCallback(async (sessionID = contextRef.current.sessionID) => {
    try {
      const status = await loadMemoryStatus(sessionID);
      dispatch({ type: 'SET_MEMORY_STATUS', segments: status.segments, jobs: status.jobs });
    } catch {
      // Memory status is diagnostic UI; chat should continue if it is unavailable.
    }
  }, [contextRef, dispatch]);

  const refreshApprovals = useCallback(async (sessionID = contextRef.current.sessionID) => {
    if (!sessionID) {
      dispatch({ type: 'SET_APPROVALS', approvals: [] });
      return;
    }
    try {
      dispatch({ type: 'SET_APPROVALS', approvals: await loadSessionApprovals(sessionID) });
    } catch {
      dispatch({ type: 'SET_APPROVALS', approvals: [] });
    }
  }, [contextRef, dispatch]);

  const refreshSessions = useCallback(async (personaKey = contextRef.current.personaKey) => {
    if (!personaKey) {
      dispatch({ type: 'SET_SESSIONS', sessions: [] });
      return;
    }
    try {
      dispatch({ type: 'SET_SESSIONS', sessions: await loadSessions(personaKey) });
    } catch (error) {
      dispatch({ type: 'SET_SESSIONS', sessions: [] });
      dispatch({ type: 'SET_STATUS', status: error instanceof Error ? error.message : 'Failed to load sessions' });
    }
  }, [contextRef, dispatch]);

  const reloadSessionHistory = useCallback(async (sessionID = contextRef.current.sessionID) => {
    if (!sessionID) return;
    const detail = await loadSessionDetail(sessionID);
    dispatch({ type: 'SET_HISTORY', messages: detail.messages || detail.Messages || [] });
  }, [contextRef, dispatch]);

  useEffect(() => {
    let cancelled = false;
    async function bootstrapChat() {
      dispatch({ type: 'SET_STATUS', status: 'Loading...' });
      const params = new URLSearchParams(location.search);
      let personaKey = params.get('persona') || '';
      let sessionID = params.get('session_id') || '';
      if (sessionID) {
        try {
          const detail = await loadSessionDetail(sessionID);
          personaKey = detail.persona || detail.Persona || personaKey;
          if (!cancelled) {
            dispatch({ type: 'SET_CONTEXT', sessionID, personaKey });
            contextRef.current = { sessionID, personaKey };
            dispatch({ type: 'SET_HISTORY', messages: detail.messages || detail.Messages || [] });
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
        dispatch({ type: 'SET_STATUS', status: 'Ready' });
      }
    }
    bootstrapChat().catch(error => {
      dispatch({ type: 'SET_STATUS', status: error instanceof Error ? error.message : 'Failed to initialize chat' });
      dispatch({ type: 'ADD_MESSAGE', role: 'error', content: error instanceof Error ? error.message : 'Failed to initialize chat' });
    });
    return () => {
      cancelled = true;
    };
  }, [contextRef, dispatch, refreshApprovals, refreshMemoryStatus, refreshSessions]);

  const startNewChat = useCallback(async () => {
    await closeSocketRef.current();
    dispatch({ type: 'SET_CONTEXT', sessionID: '' });
    contextRef.current = { ...contextRef.current, sessionID: '' };
    dispatch({ type: 'CLEAR_TIMELINE' });
    dispatch({ type: 'SET_MEMORY_STATUS', segments: [], jobs: [] });
    dispatch({ type: 'SET_STATUS', status: 'Ready' });
    await refreshSessions();
  }, [closeSocketRef, contextRef, dispatch, refreshSessions]);

  const switchSession = useCallback(async (sessionID: string, personaKey: string) => {
    dispatch({ type: 'SET_STATUS', status: 'Loading session...' });
    try {
      const detail = await loadSessionDetail(sessionID);
      await closeSocketRef.current();
      const nextPersona = detail.persona || detail.Persona || personaKey || contextRef.current.personaKey;
      dispatch({ type: 'SET_CONTEXT', sessionID, personaKey: nextPersona });
      contextRef.current = { sessionID, personaKey: nextPersona };
      dispatch({ type: 'SET_HISTORY', messages: detail.messages || detail.Messages || [] });
      await Promise.all([refreshApprovals(sessionID), refreshMemoryStatus(sessionID), refreshSessions(nextPersona)]);
      setSidebarOpen(false);
      dispatch({ type: 'SET_STATUS', status: 'Ready' });
    } catch (error) {
      dispatch({ type: 'SET_STATUS', status: error instanceof Error ? error.message : 'Failed to load session' });
    }
  }, [closeSocketRef, contextRef, dispatch, refreshApprovals, refreshMemoryStatus, refreshSessions, setSidebarOpen]);

  const removeSession = useCallback(async (sessionID: string) => {
    if (!sessionID || !window.confirm('Delete this session?')) return;
    try {
      await deleteSession(sessionID);
      if (sessionID === contextRef.current.sessionID) await startNewChat();
      else await refreshSessions();
    } catch (error) {
      dispatch({ type: 'SET_STATUS', status: error instanceof Error ? error.message : 'Failed to delete session' });
    }
  }, [contextRef, dispatch, refreshSessions, startNewChat]);

  return { refreshSessions, refreshApprovals, refreshMemoryStatus, reloadSessionHistory, startNewChat, switchSession, removeSession };
}
