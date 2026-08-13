import type { AnyRecord } from '../../shared/lib/api';
import type { ConversationEventRecord, MessageDisplayPart, MessageRecord, SessionSummary } from '../protocol/sessionApi';
import type { ApprovalRequest, ContentPart, ContextStats, ReasoningActivity, ToolActivity } from '../protocol/wsTypes';
import type { MemoryJob, MemorySegment } from '../protocol/memoryApi';

export type MessageStatus = 'sent' | 'pending' | 'failed';

export type TimelineItem =
  | { kind: 'message'; id: string; role: string; content: string; createdAt: string; status?: MessageStatus; parts?: ContentPart[]; displayParts?: MessageDisplayPart[]; groupID?: string; segmentIndex?: number; segmentTotal?: number; fresh?: boolean; groupStart?: boolean; groupEnd?: boolean }
  | { kind: 'approval'; id: string; approval: ApprovalRequest; createdAt: string }
  | { kind: 'tool'; id: string; tool: ToolActivity; createdAt: string; collapsed: boolean; fresh?: boolean }
  | { kind: 'reasoning'; id: string; reasoning: ReasoningActivity; createdAt: string; collapsed: boolean; fresh?: boolean }
  | { kind: 'work'; id: string; content: string; createdAt: string; fresh?: boolean }
  | { kind: 'memory_pipeline'; id: string; snapshot: AnyRecord; createdAt: string }
  | { kind: 'command_result'; id: string; commandID: string; commandName: string; status: string; content: string; createdAt: string; payload?: AnyRecord }
  | { kind: 'context_switched'; id: string; reason: string; content: string; createdAt: string; payload?: AnyRecord };

export type ChatState = {
  status: string;
  connected: boolean;
  sending: boolean;
  currentSessionId: string;
  currentPersonaKey: string;
  sessions: SessionSummary[];
  approvals: ApprovalRequest[];
  dismissedApprovals: string[];
  pendingApprovalIDs: string[];
  memorySegments: MemorySegment[];
  memoryJobs: MemoryJob[];
  memoryStatusVisible: boolean;
  timeline: TimelineItem[];
  pendingAssistantId: string;
  contextStats?: ContextStats;
};

export type ChatAction =
  | { type: 'SET_STATUS'; status: string }
  | { type: 'SET_CONNECTED'; connected: boolean }
  | { type: 'SET_SENDING'; sending: boolean }
  | { type: 'SET_CONTEXT'; sessionID?: string; personaKey?: string; contextStats?: ContextStats | null }
  | { type: 'SET_CONTEXT_STATS'; stats?: ContextStats | null }
  | { type: 'SET_SESSIONS'; sessions: SessionSummary[] }
  | { type: 'SET_MEMORY_STATUS'; segments: MemorySegment[]; jobs: MemoryJob[] }
  | { type: 'SET_MEMORY_VISIBLE'; visible: boolean }
  | { type: 'SET_HISTORY'; messages: MessageRecord[]; events?: ConversationEventRecord[] }
  | { type: 'CLEAR_TIMELINE' }
  | { type: 'ADD_COMMAND_RESULT'; commandID?: string; commandName?: string; status?: string; content: string; payload?: AnyRecord; createdAt?: string }
  | { type: 'ADD_CONTEXT_SWITCHED'; reason?: string; content: string; payload?: AnyRecord; createdAt?: string }
  | { type: 'ADD_MESSAGE'; role: string; content: string; id?: string; createdAt?: string; status?: MessageStatus; parts?: ContentPart[]; displayParts?: MessageDisplayPart[] }
  | { type: 'SET_MESSAGE_STATUS'; id: string; status: MessageStatus }
  | { type: 'STREAM_START' }
  | { type: 'STREAM_DELTA'; content: string }
  | { type: 'ASSISTANT_SEGMENT'; content: string; id?: string; groupID?: string; segmentIndex?: number; segmentTotal?: number; createdAt?: string }
  | { type: 'STREAM_END' }
  | { type: 'UPSERT_TOOL'; tool: ToolActivity; collapsed: boolean }
  | { type: 'UPSERT_REASONING'; reasoning: ReasoningActivity; collapsed: boolean; append: boolean; createdAt?: string }
  | { type: 'COLLAPSE_ACTIVITIES' }
  | { type: 'SET_WORK_PROGRESS'; content: string }
  | { type: 'CLEAR_WORK_PROGRESS' }
  | { type: 'SET_APPROVALS'; approvals: ApprovalRequest[] }
  | { type: 'UPSERT_APPROVAL'; approval: ApprovalRequest }
  | { type: 'DISMISS_APPROVAL'; id: string }
  | { type: 'SET_APPROVAL_PENDING'; id: string; pending: boolean };
