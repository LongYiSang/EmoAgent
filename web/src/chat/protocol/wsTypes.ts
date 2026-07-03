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
  origin?: string;
  runtime_kind?: string;
  runtimeKind?: string;
  producer_id?: string;
  producerID?: string;
  executor?: string;
  integrity?: string;
  instruction_authority?: string;
  instructionAuthority?: string;
  sensitivity?: string;
  redacted?: boolean;
  grant_ids?: string[];
  grantIDs?: string[];
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

export type ContextStats = {
  session_id?: string;
  sessionId?: string;
  turn_id?: string;
  turnId?: string;
  request_id?: string;
  requestId?: string;
  provider_id?: string;
  providerId?: string;
  model?: string;
  round?: number;
  estimated_input_tokens?: number;
  estimatedInputTokens?: number;
  context_limit_tokens?: number;
  contextLimitTokens?: number;
  input_budget_tokens?: number;
  inputBudgetTokens?: number;
  reserve_output_tokens?: number;
  reserveOutputTokens?: number;
  max_output_tokens?: number;
  maxOutputTokens?: number;
  raw_history_estimated_tokens?: number;
  rawHistoryEstimatedTokens?: number;
  compact_reason?: string;
  compactReason?: string;
  source?: string;
  provider_input_tokens?: number;
  providerInputTokens?: number;
  provider_output_tokens?: number;
  providerOutputTokens?: number;
  updated_at?: string;
  updatedAt?: string;
};

export type WSIncoming =
  | { type: 'session_ready'; session_id?: string; SessionID?: string; persona?: string; Persona?: string; origin_key?: string; OriginKey?: string; is_new?: boolean; IsNew?: boolean }
  | { type: 'greeting'; content?: string }
  | { type: 'stream_start' }
  | { type: 'stream_delta'; content?: string }
  | { type: 'assistant_segment'; content?: string; turn_id?: string; turnID?: string; group_id?: string; groupID?: string; segment_id?: string; segmentID?: string; segment_index?: number; segmentIndex?: number; segment_total?: number; segmentTotal?: number }
  | { type: 'stream_end' }
  | { type: 'command_result'; content?: string; status?: string; error_kind?: string; errorKind?: string; command_id?: string; commandID?: string; command_name?: string; commandName?: string; session_id?: string; SessionID?: string; persona?: string; Persona?: string; origin_key?: string; OriginKey?: string; payload?: AnyRecord; Payload?: AnyRecord; reload_history?: boolean; reloadHistory?: boolean; reload_memory?: boolean; reloadMemory?: boolean }
  | { type: 'context_switched'; content?: string; status?: string; session_id?: string; SessionID?: string; persona?: string; Persona?: string; origin_key?: string; OriginKey?: string; payload?: AnyRecord; Payload?: AnyRecord; reload_history?: boolean; reloadHistory?: boolean; reload_memory?: boolean; reloadMemory?: boolean }
  | { type: 'context_stats'; payload?: ContextStats; Payload?: ContextStats }
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
