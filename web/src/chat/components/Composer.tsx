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
      <button className="btn primary" id="send" type="submit" disabled={sending || !value.trim()}>发送</button>
    </form>
  );
}
