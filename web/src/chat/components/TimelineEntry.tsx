import { memo } from 'react';
import type { TimelineItem } from '../state/chatTypes';
import { MessageBubble } from './MessageBubble';
import { ApprovalCard } from './ApprovalCard';
import { ToolCard } from './ToolCard';
import { ReasoningCard } from './ReasoningCard';
import { WorkProgress } from './WorkProgress';
import { MemoryPipelineEntry } from './MemoryPipelineEntry';

export const TimelineEntry = memo(function TimelineEntry(props: {
  item: TimelineItem;
  pendingApprovalIDs: string[];
  sending: boolean;
  onApprovalAction: (id: string, action: string, optionID?: string) => void;
  onDismissApproval: (id: string) => void;
  onOpenPipeline: (snapshot: unknown) => void;
  onRetry: (message: Extract<TimelineItem, { kind: 'message' }>) => void;
}) {
  const { item } = props;
  if (item.kind === 'message') return <MessageBubble item={item} onRetry={() => props.onRetry(item)} />;
  if (item.kind === 'approval') {
    return <ApprovalCard item={item.approval} pending={props.pendingApprovalIDs.includes(item.id)} sending={props.sending} onAction={props.onApprovalAction} onDismiss={props.onDismissApproval} />;
  }
  if (item.kind === 'tool') return <ToolCard tool={item.tool} collapsed={item.collapsed} />;
  if (item.kind === 'reasoning') return <ReasoningCard reasoning={item.reasoning} collapsed={item.collapsed} onOpenPipeline={props.onOpenPipeline} />;
  if (item.kind === 'work') return <WorkProgress content={item.content} />;
  return <MemoryPipelineEntry snapshot={item.snapshot} onOpen={props.onOpenPipeline} />;
});
