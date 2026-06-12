import { useRef, useState, type ClipboardEvent, type DragEvent } from 'react';

export function Composer({
  value,
  sending,
  uploading,
  attachments,
  onChange,
  onFiles,
  onRemoveAttachment,
  onSubmit,
}: {
  value: string;
  sending: boolean;
  uploading?: boolean;
  attachments?: Array<{ id: string; label: string; preview?: string }>;
  onChange: (value: string) => void;
  onFiles?: (files: FileList | File[]) => void;
  onRemoveAttachment?: (id: string) => void;
  onSubmit: () => void;
}) {
  const dragDepth = useRef(0);
  const [dragActive, setDragActive] = useState(false);
  const disabled = sending || uploading;
  const hasInput = Boolean(value.trim()) || Boolean(attachments?.length);

  const handleDragEnter = (event: DragEvent<HTMLFormElement>) => {
    if (!hasDraggedFiles(event.dataTransfer)) return;
    event.preventDefault();
    if (disabled) return;
    dragDepth.current += 1;
    setDragActive(true);
  };

  const handleDragOver = (event: DragEvent<HTMLFormElement>) => {
    if (!hasDraggedFiles(event.dataTransfer)) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = disabled ? 'none' : 'copy';
    if (disabled) return;
    setDragActive(true);
  };

  const handleDragLeave = (event: DragEvent<HTMLFormElement>) => {
    if (!hasDraggedFiles(event.dataTransfer)) return;
    event.preventDefault();
    if (disabled) return;
    dragDepth.current = Math.max(0, dragDepth.current - 1);
    if (dragDepth.current === 0) setDragActive(false);
  };

  const handleDrop = (event: DragEvent<HTMLFormElement>) => {
    if (!hasDraggedFiles(event.dataTransfer)) return;
    event.preventDefault();
    dragDepth.current = 0;
    setDragActive(false);
    if (disabled) return;
    if (event.dataTransfer.files.length) onFiles?.(event.dataTransfer.files);
  };

  const handlePaste = (event: ClipboardEvent<HTMLTextAreaElement>) => {
    if (disabled) return;
    const files = imageFilesFromClipboard(event.clipboardData);
    if (!files.length) return;
    event.preventDefault();
    onFiles?.(files);
  };

  return (
    <form
      className={dragActive ? 'composer drag-active' : 'composer'}
      id="composer"
      onSubmit={event => { event.preventDefault(); onSubmit(); }}
      onDragEnter={handleDragEnter}
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
    >
      <div className="composer-main">
        {attachments?.length ? (
          <div className="composer-attachments">
            {attachments.map(item => (
              <span className="attachment-chip" key={item.id}>
                {item.preview ? (
                  <img src={item.preview} className="attachment-thumb" alt="" aria-hidden="true" />
                ) : null}
                <span>{item.label}</span>
                <button type="button" onClick={() => onRemoveAttachment?.(item.id)} aria-label="移除图片">×</button>
              </span>
            ))}
          </div>
        ) : null}
      <textarea
        id="input"
        value={value}
        rows={1}
        disabled={disabled}
        placeholder="和 Emotion 说点什么..."
        onChange={event => onChange(event.target.value)}
        onPaste={handlePaste}
        onKeyDown={event => {
          if (event.key === 'Enter' && !event.shiftKey) {
            event.preventDefault();
            event.currentTarget.form?.requestSubmit();
          }
        }}
      />
      </div>
      <label className="btn icon composer-upload" aria-label="上传图片" title="上传图片">
        <input
          type="file"
          accept="image/png,image/jpeg"
          multiple
          disabled={disabled}
          onChange={event => {
            if (event.currentTarget.files?.length) onFiles?.(event.currentTarget.files);
            event.currentTarget.value = '';
          }}
        />
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.3" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <rect x="3" y="5" width="18" height="14" rx="2" />
          <circle cx="8.5" cy="10" r="1.5" />
          <path d="M21 15l-5-5L5 19" />
        </svg>
      </label>
      <button
        className="btn primary icon"
        id="send"
        type="submit"
        disabled={disabled || !hasInput}
        aria-label="发送"
      >
        <svg
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2.5"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <path d="M22 2L11 13" />
          <path d="M22 2l-7 20-4-9-9-4 20-7z" />
        </svg>
      </button>
    </form>
  );
}

function hasDraggedFiles(dataTransfer: DataTransfer): boolean {
  return Array.from(dataTransfer.types || []).includes('Files');
}

function imageFilesFromClipboard(data: DataTransfer): File[] {
  const files: File[] = [];
  for (const item of Array.from(data.items || [])) {
    if (item.kind !== 'file' || !item.type.startsWith('image/')) continue;
    const file = item.getAsFile();
    if (file) files.push(file);
  }
  if (files.length) return files;
  return Array.from(data.files || []).filter(file => file.type.startsWith('image/'));
}
