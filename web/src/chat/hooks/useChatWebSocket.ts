import { useCallback, useEffect, useRef } from 'react';
import type { Dispatch, MutableRefObject } from 'react';
import type { ChatAction } from '../state/chatTypes';
import type { ReasoningActivity, WSIncoming, WSOutgoing } from '../protocol/wsTypes';

export type ChatWebSocketControls = {
  ensureConnected: () => Promise<void>;
  sendWS: (message: WSOutgoing) => void;
  closeSocket: () => Promise<void>;
};

type ConnectAttempt = {
  promise: Promise<void>;
  resolve: () => void;
  reject: (error: Error) => void;
  awaitingSessionReady: boolean;
};

type UseChatWebSocketOptions = {
  dispatch: Dispatch<ChatAction>;
  contextRef: MutableRefObject<{ personaKey: string; sessionID: string }>;
  refreshSessions: (personaKey?: string) => Promise<void>;
  refreshApprovals: (sessionID?: string) => Promise<void>;
  refreshMemoryStatus: (sessionID?: string) => Promise<void>;
  reloadSessionHistory: (sessionID?: string) => Promise<void>;
};

export function useChatWebSocket({ dispatch, contextRef, refreshSessions, refreshApprovals, refreshMemoryStatus, reloadSessionHistory }: UseChatWebSocketOptions): ChatWebSocketControls {
  const socketRef = useRef<WebSocket | null>(null);
  const reconnectTimerRef = useRef<number | null>(null);
  const reconnectDelayRef = useRef(1000);
  const manuallyClosedRef = useRef(false);
  const skipGreetingRef = useRef(false);
  const connectAttemptRef = useRef<ConnectAttempt | null>(null);
  const streamDeltaBufferRef = useRef('');
  const streamDeltaFrameRef = useRef<number | null>(null);
  const reasoningDeltaBufferRef = useRef(new Map<string, ReasoningActivity>());
  const reasoningDeltaFrameRef = useRef<number | null>(null);

  const flushStreamDelta = useCallback(() => {
    const content = streamDeltaBufferRef.current;
    streamDeltaBufferRef.current = '';
    streamDeltaFrameRef.current = null;
    if (content) dispatch({ type: 'STREAM_DELTA', content });
  }, [dispatch]);

  const flushPendingStreamDelta = useCallback(() => {
    if (streamDeltaFrameRef.current !== null) {
      window.cancelAnimationFrame(streamDeltaFrameRef.current);
    }
    flushStreamDelta();
  }, [flushStreamDelta]);

  const clearPendingStreamDelta = useCallback(() => {
    if (streamDeltaFrameRef.current !== null) {
      window.cancelAnimationFrame(streamDeltaFrameRef.current);
      streamDeltaFrameRef.current = null;
    }
    streamDeltaBufferRef.current = '';
  }, []);

  const queueStreamDelta = useCallback((content: string) => {
    if (!content) return;
    streamDeltaBufferRef.current += content;
    if (streamDeltaFrameRef.current === null) {
      streamDeltaFrameRef.current = window.requestAnimationFrame(flushStreamDelta);
    }
  }, [flushStreamDelta]);

  const flushReasoningDeltas = useCallback(() => {
    const buffered = Array.from(reasoningDeltaBufferRef.current.values());
    reasoningDeltaBufferRef.current.clear();
    reasoningDeltaFrameRef.current = null;
    for (const reasoning of buffered) {
      dispatch({ type: 'UPSERT_REASONING', reasoning, collapsed: false, append: true });
    }
  }, [dispatch]);

  const flushPendingReasoningDeltas = useCallback(() => {
    if (reasoningDeltaFrameRef.current !== null) {
      window.cancelAnimationFrame(reasoningDeltaFrameRef.current);
    }
    flushReasoningDeltas();
  }, [flushReasoningDeltas]);

  const clearPendingReasoningDeltas = useCallback(() => {
    if (reasoningDeltaFrameRef.current !== null) {
      window.cancelAnimationFrame(reasoningDeltaFrameRef.current);
      reasoningDeltaFrameRef.current = null;
    }
    reasoningDeltaBufferRef.current.clear();
  }, []);

  const queueReasoningDelta = useCallback((reasoning: ReasoningActivity) => {
    if (!reasoning.id) return;
    const buffered = reasoningDeltaBufferRef.current.get(reasoning.id);
    reasoningDeltaBufferRef.current.set(reasoning.id, buffered ? mergeReasoningDelta(buffered, reasoning) : reasoning);
    if (reasoningDeltaFrameRef.current === null) {
      reasoningDeltaFrameRef.current = window.requestAnimationFrame(flushReasoningDeltas);
    }
  }, [flushReasoningDeltas]);

  const closeSocket = useCallback(async () => {
    if (reconnectTimerRef.current) window.clearTimeout(reconnectTimerRef.current);
    reconnectTimerRef.current = null;
    const socket = socketRef.current;
    if (!socket || socket.readyState >= WebSocket.CLOSING) {
      socketRef.current = null;
      return;
    }
    manuallyClosedRef.current = true;
    await new Promise<void>(resolve => {
      socket.addEventListener('close', () => resolve(), { once: true });
      socket.close();
      window.setTimeout(resolve, 250);
    });
    socketRef.current = null;
  }, []);

  const wsURL = useCallback(() => {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const url = new URL(proto + '//' + location.host + '/ws');
    const { personaKey, sessionID } = contextRef.current;
    if (personaKey) url.searchParams.set('persona', personaKey);
    if (sessionID) url.searchParams.set('session_id', sessionID);
    if (skipGreetingRef.current) url.searchParams.set('skip_greeting', '1');
    return url.toString();
  }, [contextRef]);

  const sendWS = useCallback((message: WSOutgoing) => {
    const socket = socketRef.current;
    if (!socket || socket.readyState !== WebSocket.OPEN) throw new Error('WebSocket 尚未连接');
    socket.send(JSON.stringify(message));
  }, []);

  const connect = useCallback(() => {
    if (reconnectTimerRef.current) window.clearTimeout(reconnectTimerRef.current);
    manuallyClosedRef.current = false;
    dispatch({ type: 'SET_STATUS', status: '正在连接...' });

    const socket = new WebSocket(wsURL());
    let handshakeFailed = false;
    socketRef.current = socket;
    skipGreetingRef.current = false;

    socket.addEventListener('open', () => {
      if (socketRef.current !== socket) return;
      reconnectDelayRef.current = 1000;
      dispatch({ type: 'SET_CONNECTED', connected: true });
      dispatch({ type: 'SET_STATUS', status: '已连接' });
    });

    socket.addEventListener('message', async event => {
      if (socketRef.current !== socket) return;
      const payload = JSON.parse(String(event.data)) as WSIncoming;
      switch (payload.type) {
        case 'session_ready': {
          const sessionID = payload.session_id || payload.SessionID || contextRef.current.sessionID;
          const personaKey = payload.persona || payload.Persona || contextRef.current.personaKey;
          dispatch({ type: 'SET_CONTEXT', sessionID, personaKey });
          contextRef.current = { sessionID, personaKey };
          if (connectAttemptRef.current?.awaitingSessionReady) {
            connectAttemptRef.current.resolve();
            connectAttemptRef.current = null;
          }
          await Promise.all([refreshSessions(personaKey), refreshApprovals(sessionID), refreshMemoryStatus(sessionID)]);
          break;
        }
        case 'greeting':
          dispatch({ type: 'ADD_MESSAGE', role: 'assistant', content: payload.content || '' });
          break;
        case 'stream_start':
          clearPendingStreamDelta();
          clearPendingReasoningDeltas();
          dispatch({ type: 'STREAM_START' });
          break;
        case 'stream_delta':
          queueStreamDelta(payload.content || '');
          break;
        case 'stream_end':
          flushPendingStreamDelta();
          flushPendingReasoningDeltas();
          dispatch({ type: 'SET_SENDING', sending: false });
          dispatch({ type: 'COLLAPSE_ACTIVITIES' });
          dispatch({ type: 'CLEAR_WORK_PROGRESS' });
          try {
            await reloadSessionHistory();
          } catch {
            // Keep streamed content visible if history reload fails.
          }
          dispatch({ type: 'STREAM_END' });
          await Promise.all([refreshSessions(), refreshApprovals(), refreshMemoryStatus()]);
          break;
        case 'reasoning_start':
          flushPendingReasoningDeltas();
          {
            const reasoning = payload.reasoning || payload.Reasoning;
            if (reasoning?.id) {
              dispatch({ type: 'UPSERT_REASONING', reasoning, collapsed: false, append: false });
            }
          }
          break;
        case 'reasoning_delta': {
          const reasoning = payload.reasoning || payload.Reasoning;
          if (reasoning?.id) queueReasoningDelta(reasoning);
          break;
        }
        case 'reasoning_end': {
          flushPendingReasoningDeltas();
          const reasoning = payload.reasoning || payload.Reasoning;
          if (reasoning?.id) {
            dispatch({
              type: 'UPSERT_REASONING',
              reasoning,
              collapsed: true,
              append: false,
            });
          }
          break;
        }
        case 'tool_call_start':
        case 'tool_call_end': {
          const tool = payload.tool || payload.Tool;
          if (tool?.id) dispatch({ type: 'UPSERT_TOOL', tool, collapsed: payload.type === 'tool_call_end' });
          break;
        }
        case 'approval_required':
        case 'approval_updated': {
          const approval = payload.approval || payload.Approval;
          if (approval) dispatch({ type: 'UPSERT_APPROVAL', approval });
          break;
        }
        case 'work_progress':
          if (payload.content?.trim()) dispatch({ type: 'SET_WORK_PROGRESS', content: payload.content });
          break;
        case 'work_progress_end':
          dispatch({ type: 'CLEAR_WORK_PROGRESS' });
          break;
        case 'error':
          flushPendingStreamDelta();
          flushPendingReasoningDeltas();
          dispatch({ type: 'ADD_MESSAGE', role: 'error', content: payload.content || '未知错误' });
          dispatch({ type: 'STREAM_END' });
          dispatch({ type: 'CLEAR_WORK_PROGRESS' });
          await Promise.all([refreshSessions(), refreshApprovals(), refreshMemoryStatus()]);
          break;
        case 'pong':
          break;
      }
    });

    socket.addEventListener('close', () => {
      if (socketRef.current !== socket) return;
      flushPendingStreamDelta();
      flushPendingReasoningDeltas();
      dispatch({ type: 'SET_CONNECTED', connected: false });
      dispatch({ type: 'STREAM_END' });
      dispatch({ type: 'CLEAR_WORK_PROGRESS' });
      const pendingHandshake = Boolean(connectAttemptRef.current?.awaitingSessionReady) || handshakeFailed;
      if (pendingHandshake && connectAttemptRef.current) {
        connectAttemptRef.current.reject(new Error('连接失败'));
        connectAttemptRef.current = null;
      }
      socketRef.current = null;
      if (manuallyClosedRef.current) return;
      if (pendingHandshake) {
        dispatch({ type: 'SET_STATUS', status: '连接失败' });
        return;
      }
      dispatch({ type: 'SET_STATUS', status: '连接已断开，' + Math.round(reconnectDelayRef.current / 1000) + ' 秒后重试' });
      reconnectTimerRef.current = window.setTimeout(() => {
        reloadSessionHistory().catch(() => undefined);
        refreshApprovals().catch(() => undefined);
        refreshMemoryStatus().catch(() => undefined);
        connect();
      }, reconnectDelayRef.current);
      reconnectDelayRef.current = Math.min(reconnectDelayRef.current * 2, 30000);
    });

    socket.addEventListener('error', () => {
      if (socketRef.current !== socket) return;
      if (connectAttemptRef.current?.awaitingSessionReady) {
        handshakeFailed = true;
        connectAttemptRef.current.reject(new Error('连接异常'));
        connectAttemptRef.current = null;
      }
      dispatch({ type: 'SET_STATUS', status: '连接异常' });
    });
  }, [clearPendingReasoningDeltas, clearPendingStreamDelta, contextRef, dispatch, flushPendingReasoningDeltas, flushPendingStreamDelta, queueReasoningDelta, queueStreamDelta, refreshApprovals, refreshMemoryStatus, refreshSessions, reloadSessionHistory, wsURL]);

  const ensureConnected = useCallback(async () => {
    if (socketRef.current?.readyState === WebSocket.OPEN) return;
    if (connectAttemptRef.current?.promise) return connectAttemptRef.current.promise;
    await closeSocket();
    skipGreetingRef.current = true;
    let resolveFn!: () => void;
    let rejectFn!: (error: Error) => void;
    const promise = new Promise<void>((resolve, reject) => {
      resolveFn = resolve;
      rejectFn = reject;
    });
    connectAttemptRef.current = { promise, resolve: resolveFn, reject: rejectFn, awaitingSessionReady: true };
    connect();
    return promise;
  }, [closeSocket, connect]);

  useEffect(() => {
    const close = () => {
      flushPendingStreamDelta();
      flushPendingReasoningDeltas();
      manuallyClosedRef.current = true;
      if (reconnectTimerRef.current) window.clearTimeout(reconnectTimerRef.current);
      if (socketRef.current?.readyState && socketRef.current.readyState < WebSocket.CLOSING) socketRef.current.close();
    };
    window.addEventListener('beforeunload', close);
    return () => {
      window.removeEventListener('beforeunload', close);
      close();
    };
  }, [flushPendingReasoningDeltas, flushPendingStreamDelta]);

  return { ensureConnected, sendWS, closeSocket };
}

function mergeReasoningDelta(current: ReasoningActivity, incoming: ReasoningActivity): ReasoningActivity {
  return {
    ...current,
    ...incoming,
    content: (current.content || '') + (incoming.content || ''),
  };
}
