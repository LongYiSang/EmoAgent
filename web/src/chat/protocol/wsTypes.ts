import type { AnyRecord } from '../../shared/lib/api';

export type ToolActivity = {
  id: string;
  name: string;
  status: string;
  duration_ms?: number;
  durationMS?: number;
  preview?: string;
  size?: number;
  hash?: string;
  is_truncated?: boolean;
  isTruncated?: boolean;
};

export type ReasoningActivity = {
  id: string;
  status: string;
  content?: string;
  duration_ms?: number;
  durationMS?: number;
  provider?: string;
  model?: string;
  kind?: string;
  memory_pipeline?: AnyRecord;
  memoryPipeline?: AnyRecord;
};

export type ApprovalOption = {
  id?: string;
  summary?: string;
  [key: string]: unknown;
};

export type ApprovalRequest = {
  id?: string;
  status?: string;
  question?: string;
  options?: ApprovalOption[];
  reject_option_id?: string;
  rejectOptionID?: string;
  selected_option_id?: string;
  selectedOptionID?: string;
  goal_summary?: string;
  goalSummary?: string;
  recommendation_reason?: string;
  recommendationReason?: string;
  expires_at?: string;
  expiresAt?: string;
  created_at?: string;
  createdAt?: string;
  [key: string]: unknown;
};

export type MediaPart = {
  media_asset_id: string;
  kind: string;
  mime_type: string;
  detail?: 'auto' | 'low' | 'high' | string;
};

export type ContentPart =
  | { type: 'text'; text: string }
  | { type: 'image'; media: MediaPart };

export type WSIncoming =
  | { type: 'session_ready'; session_id?: string; SessionID?: string; persona?: string; Persona?: string; is_new?: boolean; IsNew?: boolean }
  | { type: 'greeting'; content?: string }
  | { type: 'stream_start' }
  | { type: 'stream_delta'; content?: string }
  | { type: 'stream_end' }
  | { type: 'tool_call_start'; tool?: ToolActivity; Tool?: ToolActivity }
  | { type: 'tool_call_end'; tool?: ToolActivity; Tool?: ToolActivity }
  | { type: 'reasoning_start'; reasoning?: ReasoningActivity; Reasoning?: ReasoningActivity }
  | { type: 'reasoning_delta'; reasoning?: ReasoningActivity; Reasoning?: ReasoningActivity }
  | { type: 'reasoning_end'; reasoning?: ReasoningActivity; Reasoning?: ReasoningActivity }
  | { type: 'approval_required'; approval?: ApprovalRequest; Approval?: ApprovalRequest }
  | { type: 'approval_updated'; approval?: ApprovalRequest; Approval?: ApprovalRequest }
  | { type: 'work_progress'; content?: string }
  | { type: 'work_progress_end' }
  | { type: 'error'; content?: string }
  | { type: 'pong' };

export type WSOutgoing =
  | { type: 'message'; content: string; parts?: ContentPart[] }
  | { type: 'approval_action'; request_id: string; action: 'approve' | 'reject' | string; option_id?: string }
  | { type: 'ping' };
