export function JsonSavePanel({ title, id, value, output, onValue, onSave }: { title: string; id: string; value: string; output: string; onValue: (value: string) => void; onSave: () => void }) {
  return (
    <div className="section">
      <div className="hero"><div><h2>{title}</h2><div className="meta">JSON editor</div></div><button className="btn primary" id={`save-${id}`} type="button" onClick={onSave}>Save</button></div>
      <div className="field"><label htmlFor={`${id}-editor`}>{title} JSON</label><textarea id={`${id}-editor`} className="mono tall" value={value} onChange={event => onValue(event.target.value)} /></div>
      <pre className="code" id={`${id}-json`}>{output}</pre>
    </div>
  );
}
