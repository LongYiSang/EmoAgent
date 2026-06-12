import type { AnyRecord } from '../../shared/lib/api';
import type { MessageDisplayPart, MessageRecord, SessionSummary } from '../protocol/sessionApi';
import type { ApprovalRequest, ContentPart, ReasoningActivity, ToolActivity } from '../protocol/wsTypes';
import type { MemoryJob, MemorySegment } from '../protocol/memoryApi';

export type MessageStatus = 'sent' | 'pending' | 'failed';

export type TimelineItem =
  | { kind: 'message'; id: string; role: string; content: string; createdAt: string; status?: MessageStatus; parts?: ContentPart[]; displayParts?: MessageDisplayPart[] }
  | { kind: 'approval'; id: string; approval: ApprovalRequest; createdAt: string }
  | { kind: 'tool'; id: string; tool: ToolActivity; createdAt: string; collapsed: boolean }
  | { kind: 'reasoning'; id: string; reasoning: ReasoningActivity; createdAt: string; collapsed: boolean }
  | { kind: 'work'; id: string; content: string; createdAt: string }
  | { kind: 'memory_pipeline'; id: string; snapshot: AnyRecord; createdAt: string };

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
};

export type ChatAction =
  | { type: 'SET_STATUS'; status: string }
  | { type: 'SET_CONNECTED'; connected: boolean }
  | { type: 'SET_SENDING'; sending: boolean }
  | { type: 'SET_CONTEXT'; sessionID?: string; personaKey?: string }
  | { type: 'SET_SESSIONS'; sessions: SessionSummary[] }
  | { type: 'SET_MEMORY_STATUS'; segments: MemorySegment[]; jobs: MemoryJob[] }
  | { type: 'SET_MEMORY_VISIBLE'; visible: boolean }
  | { type: 'SET_HISTORY'; messages: MessageRecord[] }
  | { type: 'CLEAR_TIMELINE' }
  | { type: 'ADD_MESSAGE'; role: string; content: string; id?: string; createdAt?: string; status?: MessageStatus; parts?: ContentPart[]; displayParts?: MessageDisplayPart[] }
  | { type: 'SET_MESSAGE_STATUS'; id: string; status: MessageStatus }
  | { type: 'STREAM_START' }
  | { type: 'STREAM_DELTA'; content: string }
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
