import { field, parseMaybeJSON, stringField, timelineMillis } from '../../shared/lib/data';
import type { AnyRecord } from '../../shared/lib/api';
import type { MessageRecord } from '../protocol/sessionApi';
import type { ApprovalRequest, ReasoningActivity, ToolActivity } from '../protocol/wsTypes';
import type { ChatAction, ChatState, TimelineItem } from './chatTypes';

export const initialChatState: ChatState = {
  status: '加载中...',
  connected: false,
  sending: false,
  currentSessionId: '',
  currentPersonaKey: '',
  sessions: [],
  approvals: [],
  dismissedApprovals: [],
  pendingApprovalIDs: [],
  memorySegments: [],
  memoryJobs: [],
  memoryStatusVisible: false,
  timeline: [],
  pendingAssistantId: '',
};

export function chatReducer(state: ChatState, action: ChatAction): ChatState {
  switch (action.type) {
    case 'SET_STATUS':
      return { ...state, status: action.status };
    case 'SET_CONNECTED':
      return { ...state, connected: action.connected };
    case 'SET_SENDING':
      return { ...state, sending: action.sending };
    case 'SET_CONTEXT':
      return {
        ...state,
        currentSessionId: action.sessionID ?? state.currentSessionId,
        currentPersonaKey: action.personaKey ?? state.currentPersonaKey,
      };
    case 'SET_SESSIONS':
      return { ...state, sessions: action.sessions };
    case 'SET_MEMORY_STATUS':
      return { ...state, memorySegments: action.segments, memoryJobs: action.jobs };
    case 'SET_MEMORY_VISIBLE':
      return { ...state, memoryStatusVisible: action.visible };
    case 'SET_HISTORY':
      return { ...state, timeline: historyToTimeline(action.messages), pendingAssistantId: '' };
    case 'CLEAR_TIMELINE':
      return { ...state, timeline: [], approvals: [], pendingAssistantId: '' };
    case 'ADD_MESSAGE': {
      const item: TimelineItem = {
        kind: 'message',
        id: action.id || crypto.randomUUID(),
        role: action.role,
        content: action.content,
        createdAt: action.createdAt || new Date().toISOString(),
        status: action.status,
        parts: action.parts,
      };
      return { ...state, timeline: orderTimeline([...state.timeline, item]) };
    }
    case 'SET_MESSAGE_STATUS':
      return {
        ...state,
        timeline: state.timeline.map(item => item.kind === 'message' && item.id === action.id ? { ...item, status: action.status } : item),
      };
    case 'STREAM_START':
      return { ...state, sending: true, pendingAssistantId: '' };
    case 'STREAM_DELTA': {
      if (!action.content) return state;
      if (state.pendingAssistantId) {
        return { ...state, timeline: appendMessageContent(state.timeline, state.pendingAssistantId, action.content) };
      }
      const id = crypto.randomUUID();
      const item: TimelineItem = {
        kind: 'message',
        id,
        role: 'assistant',
        content: action.content,
        createdAt: new Date().toISOString(),
      };
      return { ...state, pendingAssistantId: id, timeline: orderTimeline([...state.timeline, item]) };
    }
    case 'STREAM_END':
      return { ...state, sending: false, pendingAssistantId: '', pendingApprovalIDs: [] };
    case 'UPSERT_TOOL':
      return { ...state, timeline: upsertItem(state.timeline, toolToItem(action.tool, action.collapsed)) };
    case 'UPSERT_REASONING':
      return { ...state, timeline: upsertReasoning(state.timeline, action.reasoning, action.collapsed, action.append, action.createdAt) };
    case 'COLLAPSE_ACTIVITIES':
      return {
        ...state,
        timeline: state.timeline.map(item => {
          if (item.kind === 'tool' || item.kind === 'reasoning') return { ...item, collapsed: true };
          return item;
        }),
      };
    case 'SET_WORK_PROGRESS':
      return { ...state, timeline: upsertItem(state.timeline, { kind: 'work', id: 'work-progress', content: action.content, createdAt: new Date().toISOString() }) };
    case 'CLEAR_WORK_PROGRESS':
      return { ...state, timeline: state.timeline.filter(item => item.kind !== 'work') };
    case 'SET_APPROVALS':
      return {
        ...state,
        approvals: mergeApprovals(action.approvals),
        timeline: orderTimeline([
          ...state.timeline.filter(item => item.kind !== 'approval'),
          ...visibleApprovals(action.approvals, state.dismissedApprovals).map(approvalToItem),
        ]),
      };
    case 'UPSERT_APPROVAL': {
      const approvals = mergeApprovals([...state.approvals, action.approval]);
      return {
        ...state,
        approvals,
        timeline: orderTimeline([
          ...state.timeline.filter(item => item.kind !== 'approval'),
          ...visibleApprovals(approvals, state.dismissedApprovals).map(approvalToItem),
        ]),
      };
    }
    case 'DISMISS_APPROVAL': {
      const dismissedApprovals = Array.from(new Set([...state.dismissedApprovals, action.id]));
      return {
        ...state,
        dismissedApprovals,
        timeline: state.timeline.filter(item => item.kind !== 'approval' || item.id !== action.id),
      };
    }
    case 'SET_APPROVAL_PENDING': {
      const set = new Set(state.pendingApprovalIDs);
      if (action.pending) set.add(action.id);
      else set.delete(action.id);
      return { ...state, pendingApprovalIDs: Array.from(set) };
    }
    default:
      return state;
  }
}

function historyToTimeline(messages: MessageRecord[]): TimelineItem[] {
  const items: TimelineItem[] = [];
  for (const message of messages || []) {
    const role = stringField(message, 'role') || 'assistant';
    const content = stringField(message, 'content');
    const createdAt = stringField(message, 'created_at') || stringField(message, 'createdAt') || new Date().toISOString();
    const id = stringField(message, 'id') || crypto.randomUUID();
    const metadata = parseMaybeJSON(field(message, 'metadata', {}));
    if (role === 'assistant') {
      items.push(...thinkingBlocksToTimeline(metadata, createdAt, id));
    }
    if (content) {
      items.push({ kind: 'message', id, role, content, createdAt });
    }
  }
  return orderTimeline(items);
}

function thinkingBlocksToTimeline(metadata: AnyRecord, createdAt: string, messageID: string): TimelineItem[] {
  const blocks = Array.isArray(metadata.thinking_blocks) ? metadata.thinking_blocks : Array.isArray(metadata.thinkingBlocks) ? metadata.thinkingBlocks : [];
  const memoryPipeline = metadata.memory_pipeline || metadata.memoryPipeline;
  if (!blocks.length && memoryPipeline && typeof memoryPipeline === 'object') {
    return [{ kind: 'memory_pipeline', id: `${messageID}:memory-pipeline`, snapshot: memoryPipeline as AnyRecord, createdAt }];
  }
  return blocks.map((block, index) => {
    const rawID = stringField(block, 'id') || String(index + 1);
    const reasoning = {
      ...(typeof block === 'object' && block ? block as ReasoningActivity : {}),
      id: `${messageID}:${rawID}`,
      status: 'done',
      ...(index === 0 && memoryPipeline && typeof memoryPipeline === 'object' ? { memory_pipeline: memoryPipeline as AnyRecord } : {}),
    };
    return { kind: 'reasoning', id: reasoning.id, reasoning, createdAt, collapsed: true } satisfies TimelineItem;
  });
}

function toolToItem(tool: ToolActivity, collapsed: boolean): TimelineItem {
  return { kind: 'tool', id: tool.id, tool, collapsed, createdAt: new Date().toISOString() };
}

function approvalToItem(approval: ApprovalRequest): TimelineItem {
  const id = stringField(approval, 'id');
  return {
    kind: 'approval',
    id,
    approval,
    createdAt: stringField(approval, 'created_at') || stringField(approval, 'createdAt') || new Date().toISOString(),
  };
}

function upsertItem(timeline: TimelineItem[], item: TimelineItem): TimelineItem[] {
  const next = timeline.filter(existing => existing.kind !== item.kind || existing.id !== item.id);
  next.push(item);
  return orderTimeline(next);
}

function upsertReasoning(timeline: TimelineItem[], incoming: ReasoningActivity, collapsed: boolean, append: boolean, createdAt?: string): TimelineItem[] {
  const index = timeline.findIndex(item => item.kind === 'reasoning' && item.id === incoming.id);
  const current = index >= 0 ? timeline[index] : undefined;
  const previous = current?.kind === 'reasoning' ? current.reasoning : undefined;
  const previousContent = previous?.content || '';
  const incomingContent = incoming.content || '';
  const content = incomingContent ? (append ? previousContent + incomingContent : incomingContent) : previousContent;
  const item: TimelineItem = {
    kind: 'reasoning',
    id: incoming.id,
    reasoning: { ...(previous || {}), ...incoming, content },
    collapsed,
    createdAt: current?.createdAt || createdAt || new Date().toISOString(),
  };
  if (index < 0) return orderTimeline([...timeline, item]);
  const next = timeline.slice();
  next[index] = item;
  return next;
}

function appendMessageContent(timeline: TimelineItem[], id: string, content: string): TimelineItem[] {
  const index = timeline.findIndex(item => item.kind === 'message' && item.id === id);
  if (index < 0) return timeline;
  const item = timeline[index];
  if (item.kind !== 'message') return timeline;
  const next = timeline.slice();
  next[index] = { ...item, content: item.content + content };
  return next;
}

function orderTimeline(items: TimelineItem[]): TimelineItem[] {
  return [...items].sort((a, b) => timelineMillis(a.createdAt) - timelineMillis(b.createdAt));
}

function mergeApprovals(items: ApprovalRequest[]): ApprovalRequest[] {
  const map = new Map<string, ApprovalRequest>();
  for (const item of items || []) {
    const id = stringField(item, 'id');
    if (id) map.set(id, item);
  }
  return Array.from(map.values());
}

function visibleApprovals(items: ApprovalRequest[], dismissed: string[]): ApprovalRequest[] {
  const dismissedSet = new Set(dismissed);
  return items.filter(item => {
    const id = stringField(item, 'id');
    const status = stringField(item, 'status') || 'pending';
    return !!id && (status === 'pending' || !dismissedSet.has(id));
  });
}
