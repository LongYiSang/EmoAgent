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
  attachments?: Array<{ id: string; label: string }>;
  onChange: (value: string) => void;
  onFiles?: (files: FileList) => void;
  onRemoveAttachment?: (id: string) => void;
  onSubmit: () => void;
}) {
  const hasInput = Boolean(value.trim()) || Boolean(attachments?.length);
  return (
    <form className="composer" id="composer" onSubmit={event => { event.preventDefault(); onSubmit(); }}>
      <div className="composer-main">
        {attachments?.length ? (
          <div className="composer-attachments">
            {attachments.map(item => (
              <span className="attachment-chip" key={item.id}>
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
        disabled={sending || uploading}
        placeholder="和 Emotion 说点什么..."
        onChange={event => onChange(event.target.value)}
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
          disabled={sending || uploading}
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
        disabled={sending || uploading || !hasInput}
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
