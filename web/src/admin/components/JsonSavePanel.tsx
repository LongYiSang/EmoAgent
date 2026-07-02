export function JsonSavePanel({ title, id, value, output, onValue, onSave }: { title: string; id: string; value: string; output: string; onValue: (value: string) => void; onSave: () => void }) {
  return (
    <div className="section">
      <div className="hero">
        <div>
          <h2>{title}</h2>
          <div className="meta">高级 JSON 配置（直接编辑）</div>
        </div>
        <button className="btn primary" id={`save-${id}`} type="button" onClick={onSave}>保存</button>
      </div>

      <div className="slot" style={{ marginBottom: 12 }}>
        <div className="slot-head"><strong>配置编辑</strong></div>
        <div className="field">
          <label htmlFor={`${id}-editor`}>{title} JSON</label>
          <textarea id={`${id}-editor`} className="mono tall" value={value} onChange={event => onValue(event.target.value)} />
        </div>
      </div>

      <div className="slot">
        <div className="slot-head"><strong>当前生效 / 实时状态（只读）</strong></div>
        <pre className="code" id={`${id}-json`}>{output}</pre>
      </div>
    </div>
  );
}
