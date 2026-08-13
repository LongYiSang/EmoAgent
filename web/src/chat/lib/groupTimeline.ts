import type { TimelineItem } from '../state/chatTypes';

type MessageItem = Extract<TimelineItem, { kind: 'message' }>;

/**
 * Widest gap between two consecutive same-role messages that still reads as one
 * turn. Only used for messages that carry no explicit group id — reply-delivery
 * segments always do, so this is the fallback for plain consecutive sends.
 */
const GROUP_WINDOW_MS = 60_000;

function isMessage(item: TimelineItem): item is MessageItem {
  return item.kind === 'message';
}

function sameTurn(prev: MessageItem, next: MessageItem): boolean {
  if (prev.role !== next.role) return false;

  // Reply delivery splits one reply into segments and tags them with a shared
  // groupID — trust it rather than guessing from timestamps.
  if (prev.groupID || next.groupID) return Boolean(prev.groupID) && prev.groupID === next.groupID;

  const prevAt = Date.parse(prev.createdAt);
  const nextAt = Date.parse(next.createdAt);
  if (Number.isNaN(prevAt) || Number.isNaN(nextAt)) return false;
  return Math.abs(nextAt - prevAt) <= GROUP_WINDOW_MS;
}

/**
 * Marks each message as the start and/or end of a run of consecutive messages
 * from the same turn, so the renderer can draw one avatar and one timestamp per
 * turn instead of one per segment. Anything that is not a message (tool cards,
 * approvals, reasoning) breaks the run.
 *
 * Pure and non-mutating: non-message items pass through by reference.
 */
export function annotateMessageGroups(items: TimelineItem[]): TimelineItem[] {
  return items.map((item, index) => {
    if (!isMessage(item)) return item;

    const previous = items[index - 1];
    const next = items[index + 1];
    const groupStart = !(previous && isMessage(previous) && sameTurn(previous, item));
    const groupEnd = !(next && isMessage(next) && sameTurn(item, next));

    if (item.groupStart === groupStart && item.groupEnd === groupEnd) return item;
    return { ...item, groupStart, groupEnd };
  });
}
