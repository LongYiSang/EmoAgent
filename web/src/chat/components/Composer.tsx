export function Composer({
  value,
  sending,
  onChange,
  onSubmit,
}: {
  value: string;
  sending: boolean;
  onChange: (value: string) => void;
  onSubmit: () => void;
}) {
  return (
    <form className="composer" id="composer" onSubmit={event => { event.preventDefault(); onSubmit(); }}>
      <textarea
        id="input"
        value={value}
        rows={1}
        disabled={sending}
        placeholder="和 Emotion 说点什么..."
        onChange={event => onChange(event.target.value)}
        onKeyDown={event => {
          if (event.key === 'Enter' && !event.shiftKey) {
            event.preventDefault();
            event.currentTarget.form?.requestSubmit();
          }
        }}
      />
      <button
        className="btn primary icon"
        id="send"
        type="submit"
        disabled={sending || !value.trim()}
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
