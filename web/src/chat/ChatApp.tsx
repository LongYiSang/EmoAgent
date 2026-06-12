import { useCallback, useEffect, useReducer, useRef, useState } from 'react';
import { AppRail } from '../shared/components/AppRail';
import { queueMemoryExtraction } from './protocol/memoryApi';
import { uploadMedia, type UploadedMedia } from './protocol/mediaApi';
import type { MessageDisplayPart } from './protocol/sessionApi';
import type { ContentPart, WSOutgoing } from './protocol/wsTypes';
import type { TimelineItem } from './state/chatTypes';
import { chatReducer, initialChatState } from './state/chatReducer';
import { Composer } from './components/Composer';
import { ConversationHeader } from './components/ConversationHeader';
import { MemoryStatusPanel } from './components/MemoryStatusPanel';
import { PipelinePanel } from './components/PipelinePanel';
import { SessionSidebar } from './components/SessionSidebar';
import { VirtualTimeline } from './components/VirtualTimeline';
import { syncURL } from './lib/chatViewData';
import { useChatSession } from './hooks/useChatSession';
import { useChatWebSocket } from './hooks/useChatWebSocket';
import '../styles.css';

type DraftAttachment = UploadedMedia & {
  preview_url?: string;
};

export function ChatApp() {
  const [state, dispatch] = useReducer(chatReducer, initialChatState);
  const [composer, setComposer] = useState('');
  const [attachments, setAttachments] = useState<DraftAttachment[]>([]);
  const [uploading, setUploading] = useState(false);
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [pipelineSnapshot, setPipelineSnapshot] = useState<unknown>(null);
  const contextRef = useRef({ personaKey: '', sessionID: '' });
  const closeSocketRef = useRef<() => Promise<void>>(async () => undefined);
  const previewURLsRef = useRef(new Set<string>());

  useEffect(() => {
    contextRef.current = { personaKey: state.currentPersonaKey, sessionID: state.currentSessionId };
    syncURL(state.currentPersonaKey, state.currentSessionId);
  }, [state.currentPersonaKey, state.currentSessionId]);

  const session = useChatSession({ state, dispatch, contextRef, closeSocketRef, setSidebarOpen });
  const { ensureConnected, sendWS, closeSocket } = useChatWebSocket({
    dispatch,
    contextRef,
    refreshSessions: session.refreshSessions,
    refreshApprovals: session.refreshApprovals,
    refreshMemoryStatus: session.refreshMemoryStatus,
    reloadSessionHistory: session.reloadSessionHistory,
  });

  useEffect(() => {
    closeSocketRef.current = closeSocket;
  }, [closeSocket]);

  useEffect(() => {
    const activePreviewURLs = new Set<string>();
    for (const attachment of attachments) {
      if (attachment.preview_url) activePreviewURLs.add(attachment.preview_url);
    }
    for (const item of state.timeline) {
      if (item.kind !== 'message') continue;
      for (const part of item.displayParts || []) {
        if (part.type === 'image' && part.display_url?.startsWith('blob:')) {
          activePreviewURLs.add(part.display_url);
        }
      }
    }
    for (const url of Array.from(previewURLsRef.current)) {
      if (activePreviewURLs.has(url)) continue;
      URL.revokeObjectURL(url);
      previewURLsRef.current.delete(url);
    }
  }, [attachments, state.timeline]);

  useEffect(() => () => {
    for (const url of previewURLsRef.current) {
      URL.revokeObjectURL(url);
    }
    previewURLsRef.current.clear();
  }, []);

  const sendMessage = useCallback(async (content: string, localID: string, parts?: ContentPart[]) => {
    dispatch({ type: 'SET_MESSAGE_STATUS', id: localID, status: 'pending' });
    dispatch({ type: 'SET_SENDING', sending: true });
    try {
      await ensureConnected();
      sendWS({ type: 'message', content, parts } satisfies WSOutgoing);
      dispatch({ type: 'SET_MESSAGE_STATUS', id: localID, status: 'sent' });
    } catch (error) {
      dispatch({ type: 'SET_MESSAGE_STATUS', id: localID, status: 'failed' });
      dispatch({ type: 'SET_SENDING', sending: false });
      dispatch({ type: 'SET_STATUS', status: error instanceof Error ? error.message : '连接失败' });
    }
  }, [ensureConnected, sendWS]);

  const submitMessage = useCallback(async () => {
    const content = composer.trim();
    if ((!content && attachments.length === 0) || state.sending || uploading) return;
    const id = crypto.randomUUID();
    let sessionID = contextRef.current.sessionID || state.currentSessionId;
    if (attachments.length > 0 && !sessionID) {
      try {
        await ensureConnected();
        sessionID = contextRef.current.sessionID || state.currentSessionId;
      } catch {
        sessionID = '';
      }
    }
    const parts: ContentPart[] = [
      ...(content ? [{ type: 'text', text: content } satisfies ContentPart] : []),
      ...attachments.map(asset => ({
        type: 'image',
        media: {
          media_asset_id: asset.media_asset_id,
          kind: asset.kind,
          mime_type: asset.mime_type,
          detail: 'auto',
        },
      } satisfies ContentPart)),
    ];
    const visibleContent = [content, ...attachments.map(() => '[used image]')].filter(Boolean).join('\n');
    const displayParts = outgoingDisplayParts(content, attachments, sessionID);
    dispatch({ type: 'ADD_MESSAGE', id, role: 'user', content: visibleContent, createdAt: new Date().toISOString(), status: 'pending', parts, displayParts });
    setComposer('');
    setAttachments([]);
    await sendMessage(content || visibleContent, id, parts);
  }, [attachments, composer, ensureConnected, sendMessage, state.currentSessionId, state.sending, uploading]);

  const handleFiles = useCallback(async (files: FileList | File[]) => {
    if (state.sending || uploading) return;
    const allFiles = Array.from(files);
    if (!allFiles.length) return;
    const imageFiles = supportedImageFiles(allFiles);
    if (!imageFiles.length) {
      dispatch({ type: 'SET_STATUS', status: '仅支持 PNG/JPEG 图片' });
      return;
    }
    const skippedUnsupported = imageFiles.length < allFiles.length;
    setUploading(true);
    dispatch({ type: 'SET_STATUS', status: '上传图片...' });
    const createdPreviewURLs: string[] = [];
    try {
      // Process uploads in parallel for better UX with multiple images.
      // (Previously had client-side resizing for performance, but that feature is disabled for now
      //  to preserve original image quality when sending to the AI.)
      const tasks = imageFiles.map(async (file) => {
        const previewURL = URL.createObjectURL(file);
        createdPreviewURLs.push(previewURL);
        const asset = await uploadMedia(file); // use original file directly
        return { ...asset, preview_url: previewURL } as DraftAttachment;
      });
      const uploaded = await Promise.all(tasks);
      for (const attachment of uploaded) {
        if (attachment.preview_url) previewURLsRef.current.add(attachment.preview_url);
      }
      setAttachments(current => [...current, ...uploaded]);
      dispatch({ type: 'SET_STATUS', status: skippedUnsupported ? '已忽略非 PNG/JPEG 图片' : '' });
    } catch (error) {
      // Revoke any previews created in this batch on failure
      for (const url of createdPreviewURLs) {
        URL.revokeObjectURL(url);
      }
      dispatch({ type: 'SET_STATUS', status: error instanceof Error ? error.message : '图片上传失败' });
    } finally {
      setUploading(false);
    }
  }, [state.sending, uploading]);

  const removeAttachment = useCallback((id: string) => {
    setAttachments(current => current.filter(item => item.media_asset_id !== id));
  }, []);

  const sendApprovalAction = useCallback(async (requestID: string, action: string, optionID = '') => {
    if (!requestID || state.pendingApprovalIDs.includes(requestID)) return;
    dispatch({ type: 'SET_APPROVAL_PENDING', id: requestID, pending: true });
    try {
      await ensureConnected();
      sendWS({ type: 'approval_action', request_id: requestID, action, option_id: optionID });
    } catch (error) {
      dispatch({ type: 'SET_APPROVAL_PENDING', id: requestID, pending: false });
      dispatch({ type: 'SET_STATUS', status: error instanceof Error ? error.message : '审批操作发送失败' });
    }
  }, [ensureConnected, sendWS, state.pendingApprovalIDs]);

  const dismissApproval = useCallback((id: string) => {
    dispatch({ type: 'DISMISS_APPROVAL', id });
  }, []);

  const retryMessage = useCallback((message: Extract<TimelineItem, { kind: 'message' }>) => {
    const retryContent = message.parts ? textContentFromParts(message.parts) || message.content : message.content;
    sendMessage(retryContent, message.id, message.parts);
  }, [sendMessage]);

  const scanMemory = useCallback(async () => {
    if (!state.currentSessionId) return;
    dispatch({ type: 'SET_STATUS', status: '提交记忆扫描...' });
    try {
      await queueMemoryExtraction(state.currentSessionId);
      dispatch({ type: 'SET_STATUS', status: '记忆扫描已提交' });
      await session.refreshMemoryStatus(state.currentSessionId);
    } catch (error) {
      dispatch({ type: 'SET_STATUS', status: error instanceof Error ? error.message : '记忆扫描提交失败' });
    }
  }, [session, state.currentSessionId]);

  const subtitle = state.currentPersonaKey ? 'Persona：' + state.currentPersonaKey : 'EmoAgent';

  return (
    <div className="app-shell">
      <AppRail active="chat" />
      <main className="chat-page">
        <SessionSidebar
          open={sidebarOpen}
          sessions={state.sessions}
          currentPersonaKey={state.currentPersonaKey}
          currentSessionId={state.currentSessionId}
          onRefresh={() => session.refreshSessions()}
          onNew={session.startNewChat}
          onOpen={session.switchSession}
          onDelete={session.removeSession}
        />
        <section className="conversation">
          <ConversationHeader
            subtitle={subtitle}
            status={state.status}
            memoryStatusVisible={state.memoryStatusVisible}
            hasSession={Boolean(state.currentSessionId)}
            onToggleSidebar={() => setSidebarOpen(value => !value)}
            onToggleMemory={() => dispatch({ type: 'SET_MEMORY_VISIBLE', visible: !state.memoryStatusVisible })}
            onScanMemory={scanMemory}
          />
          <MemoryStatusPanel visible={state.memoryStatusVisible} segments={state.memorySegments} jobs={state.memoryJobs} />
          <VirtualTimeline
            items={state.timeline}
            pendingApprovalIDs={state.pendingApprovalIDs}
            sending={state.sending}
            sessionID={state.currentSessionId}
            pendingAssistantId={state.pendingAssistantId}
            onApprovalAction={sendApprovalAction}
            onDismissApproval={dismissApproval}
            onOpenPipeline={setPipelineSnapshot}
            onRetry={retryMessage}
          />
          <Composer
            value={composer}
            sending={state.sending}
            uploading={uploading}
            attachments={attachments.map(item => ({
              id: item.media_asset_id,
              label: item.original_filename || item.mime_type,
              preview: item.preview_url
            }))}
            onChange={setComposer}
            onFiles={handleFiles}
            onRemoveAttachment={removeAttachment}
            onSubmit={submitMessage}
          />
          <div className="session-hint" id="session-hint">
            {state.currentSessionId ? '会话 · ' + state.currentSessionId.substring(0, 13) : '暂无活动会话'}
          </div>
        </section>
        <PipelinePanel snapshot={pipelineSnapshot} onClose={() => setPipelineSnapshot(null)} />
      </main>
    </div>
  );
}

function textContentFromParts(parts: ContentPart[]): string {
  return parts.filter(part => part.type === 'text').map(part => part.text).join('\n').trim();
}

function outgoingDisplayParts(content: string, attachments: DraftAttachment[], sessionID: string): MessageDisplayPart[] | undefined {
  if (!content && attachments.length === 0) return undefined;
  const parts: MessageDisplayPart[] = [];
  if (content) parts.push({ type: 'text', text: content });
  for (const asset of attachments) {
    parts.push({
      type: 'image',
      media_asset_id: asset.media_asset_id,
      kind: asset.kind,
      mime_type: asset.mime_type,
      byte_size: asset.byte_size,
      width: asset.width,
      height: asset.height,
      display_url: asset.preview_url || (sessionID ? sessionMediaDisplayURL(sessionID, asset.media_asset_id) : undefined),
    });
  }
  return parts;
}

function sessionMediaDisplayURL(sessionID: string, mediaAssetID: string): string {
  return `/api/sessions/${encodeURIComponent(sessionID)}/media/${encodeURIComponent(mediaAssetID)}`;
}

function supportedImageFiles(files: File[]): File[] {
  return files.filter(file => file.type === 'image/png' || file.type === 'image/jpeg');
}
