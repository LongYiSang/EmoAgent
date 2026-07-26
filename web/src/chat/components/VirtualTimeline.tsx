import { useCallback, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import type { TimelineItem } from '../state/chatTypes';
import { TimelineEntry } from './TimelineEntry';

type VirtualTimelineProps = {
  items: TimelineItem[];
  pendingApprovalIDs: string[];
  sending: boolean;
  sessionID: string;
  pendingAssistantId: string;
  onApprovalAction: (id: string, action: string, optionID?: string) => void;
  onDismissApproval: (id: string) => void;
  onOpenPipeline: (snapshot: unknown) => void;
  onRetry: (message: Extract<TimelineItem, { kind: 'message' }>) => void;
  onStartChat?: () => void;
};

function timelineItemKey(item: TimelineItem) {
  return item.kind + ':' + item.id;
}

function estimateTimelineItemSize(item: TimelineItem | undefined) {
  if (!item) return 140;
  if (item.kind === 'message') {
    const textLen = (item.content || '').length;
    let size = 80 + Math.ceil(textLen / 40) * 20;
    // Account for images in displayParts (max ~320px tall + margins/padding)
    const imageCount = (item.displayParts || []).filter((p: any) => p && p.type === 'image').length;
    if (imageCount > 0) {
      size += imageCount * 230; // ~ image height + gap + bubble padding
    }
    return Math.min(900, Math.max(120, size));
  }
  if (item.kind === 'approval') return 260;
  if (item.kind === 'tool') return 160;
  if (item.kind === 'reasoning') return 150;
  if (item.kind === 'work') return 90;
  if (item.kind === 'command_result' || item.kind === 'context_switched') return 120;
  if (item.kind === 'memory_pipeline') return 180;
  return 140;
}

export function VirtualTimeline({
  items,
  pendingApprovalIDs,
  sending,
  sessionID,
  pendingAssistantId,
  onApprovalAction,
  onDismissApproval,
  onOpenPipeline,
  onRetry,
  onStartChat,
}: VirtualTimelineProps) {
  const parentRef = useRef<HTMLDivElement | null>(null);
  // Keys whose entrance animation already played — survives re-renders and
  // prevents replays when a virtual row unmounts and remounts on scroll.
  const animatedKeysRef = useRef(new Set<string>());
  const sessionRef = useRef(sessionID);
  const forceScrollAfterSessionChangeRef = useRef(true);
  const sessionScrollFrameRef = useRef<number | null>(null);
  const settleScrollFrameRef = useRef<number | null>(null);
  const [activityOpenByKey, setActivityOpenByKey] = useState<Record<string, boolean>>({});
  const itemKeys = useMemo(() => items.map(timelineItemKey), [items]);
  const itemSignature = itemKeys.length ? itemKeys[0] + ':' + itemKeys[itemKeys.length - 1] : '';
  const shouldFollowToBottomRef = useRef(true);

  const getItemKey = useCallback((index: number) => itemKeys[index] || index, [itemKeys]);
  const virtualizer = useVirtualizer<HTMLDivElement, HTMLDivElement>({
    count: items.length,
    getScrollElement: () => parentRef.current,
    getItemKey,
    estimateSize: index => estimateTimelineItemSize(items[index]),
    overscan: 6,
    gap: 18,
    anchorTo: 'end',
    followOnAppend: true,
    scrollEndThreshold: 48,
    directDomUpdates: true,
    directDomUpdatesMode: 'transform',
  });

  const cancelSettleScroll = useCallback(() => {
    if (settleScrollFrameRef.current !== null) {
      window.cancelAnimationFrame(settleScrollFrameRef.current);
      settleScrollFrameRef.current = null;
    }
  }, []);

  const scheduleSettleScroll = useCallback(() => {
    cancelSettleScroll();
    let remainingFrames = 3;
    const settle = () => {
      settleScrollFrameRef.current = null;
      const parent = parentRef.current;
      if (!parent || !shouldFollowToBottomRef.current) return;
      virtualizer.scrollToEnd({ behavior: 'auto' });
      parent.scrollTop = parent.scrollHeight;
      remainingFrames -= 1;
      if (remainingFrames > 0) {
        settleScrollFrameRef.current = window.requestAnimationFrame(settle);
      }
    };
    settleScrollFrameRef.current = window.requestAnimationFrame(settle);
  }, [cancelSettleScroll, virtualizer]);

  useLayoutEffect(() => {
    if (sessionRef.current === sessionID) return;
    sessionRef.current = sessionID;
    forceScrollAfterSessionChangeRef.current = true;
    shouldFollowToBottomRef.current = true;
    setActivityOpenByKey({});
    animatedKeysRef.current.clear();
  }, [sessionID]);

  useLayoutEffect(() => {
    const parent = parentRef.current;
    if (!parent) return;
    const updateShouldFollow = () => {
      const distanceFromEnd = parent.scrollHeight - parent.clientHeight - parent.scrollTop;
      shouldFollowToBottomRef.current = distanceFromEnd <= 48;
    };
    updateShouldFollow();
    parent.addEventListener('scroll', updateShouldFollow, { passive: true });
    return () => parent.removeEventListener('scroll', updateShouldFollow);
  }, []);

  useLayoutEffect(() => {
    if (sessionScrollFrameRef.current !== null) {
      window.cancelAnimationFrame(sessionScrollFrameRef.current);
      sessionScrollFrameRef.current = null;
    }
    if (!items.length || !forceScrollAfterSessionChangeRef.current) return;
    sessionScrollFrameRef.current = window.requestAnimationFrame(() => {
      sessionScrollFrameRef.current = null;
      if (!forceScrollAfterSessionChangeRef.current) return;
      virtualizer.scrollToEnd({ behavior: 'auto' });
      if (parentRef.current) parentRef.current.scrollTop = parentRef.current.scrollHeight;
      forceScrollAfterSessionChangeRef.current = false;
    });
    return () => {
      if (sessionScrollFrameRef.current !== null) {
        window.cancelAnimationFrame(sessionScrollFrameRef.current);
        sessionScrollFrameRef.current = null;
      }
    };
  }, [itemSignature, items.length, sessionID, virtualizer]);

  useLayoutEffect(() => {
    if (shouldFollowToBottomRef.current) {
      scheduleSettleScroll();
    }
  }, [items, pendingAssistantId, scheduleSettleScroll]);

  useLayoutEffect(() => () => {
    if (sessionScrollFrameRef.current !== null) window.cancelAnimationFrame(sessionScrollFrameRef.current);
    cancelSettleScroll();
  }, [cancelSettleScroll]);

  return (
    <div className="messages" id="messages" ref={parentRef}>
      {items.length === 0 && (
        <div className="timeline-empty">
          <div className="timeline-empty-emoji" aria-hidden="true">🫧</div>
          <p>还没有消息，和 Emotion 打个招呼吧</p>
          {onStartChat && <button className="btn primary" type="button" onClick={onStartChat}>开始新对话</button>}
        </div>
      )}
      <div className="timeline-virtualizer" ref={virtualizer.containerRef}>
        {virtualizer.getVirtualItems().map(virtualItem => {
          const item = items[virtualItem.index];
          if (!item) return null;
          const key = itemKeys[virtualItem.index] || timelineItemKey(item);
          const activityOpen = item.kind === 'tool' || item.kind === 'reasoning'
            ? activityOpenByKey[key] ?? !item.collapsed
            : undefined;
          const fresh = Boolean((item as { fresh?: boolean }).fresh) && !animatedKeysRef.current.has(key);
          const streaming = item.kind === 'message' && !!pendingAssistantId && item.id === pendingAssistantId;
          return (
            <div
              key={virtualItem.key}
              className="timeline-virtual-row"
              data-index={virtualItem.index}
              ref={virtualizer.measureElement}
            >
              <div
                className={fresh ? 'entry-fresh' : undefined}
                onAnimationEnd={fresh ? event => {
                  if (event.animationName === 'msg-in') animatedKeysRef.current.add(key);
                } : undefined}
              >
                <TimelineEntry
                  item={item}
                  activityOpen={activityOpen}
                  pendingApprovalIDs={pendingApprovalIDs}
                  sending={sending}
                  streaming={streaming}
                  onActivityOpenChange={open => setActivityOpenByKey(current => ({ ...current, [key]: open }))}
                  onApprovalAction={onApprovalAction}
                  onDismissApproval={onDismissApproval}
                  onOpenPipeline={onOpenPipeline}
                  onRetry={onRetry}
                />
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
