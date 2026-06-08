export function JsonSavePanel({ title, id, value, output, onValue, onSave }: { title: string; id: string; value: string; output: string; onValue: (value: string) => void; onSave: () => void }) {
  return (
    <div className="section">
      <div className="hero"><div><h2>{title}</h2><div className="meta">JSON 编辑器</div></div><button className="btn primary" id={`save-${id}`} type="button" onClick={onSave}>保存</button></div>
      <div className="field"><label htmlFor={`${id}-editor`}>{title} JSON</label><textarea id={`${id}-editor`} className="mono tall" value={value} onChange={event => onValue(event.target.value)} /></div>
      <div className="code-header">
        <span className="code-header-label">当前生效 / 实时状态（只读）</span>
      </div>
      <pre className="code" id={`${id}-json`}>{output}</pre>
    </div>
  );
}
