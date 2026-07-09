export function JsonSavePanel({
  title,
  id,
  value,
  output,
  description,
  onValue,
  onSave,
}: {
  title: string;
  id: string;
  value: string;
  output: string;
  description?: string;
  onValue: (value: string) => void;
  onSave: () => void;
}) {
  return (
    <div className="section">
      <div className="hero sticky-hero">
        <div>
          <h2>{title}</h2>
          <div className="meta">{description || '高级 JSON 配置（直接编辑）'}</div>
        </div>
        <div className="actions">
          <button className="btn primary" id={`save-${id}`} type="button" onClick={onSave}>保存</button>
        </div>
      </div>

      <div className="config-section">
        <div className="config-section-head">
          <strong>配置编辑</strong>
          <span className="meta">修改后点右上角保存；非法 JSON 将拒绝写入</span>
        </div>
        <div className="field">
          <label htmlFor={`${id}-editor`}>{title} JSON</label>
          <textarea id={`${id}-editor`} className="mono tall" value={value} onChange={event => onValue(event.target.value)} />
        </div>
      </div>

      <div className="config-section">
        <div className="config-section-head">
          <strong>当前生效</strong>
          <span className="meta">只读快照，保存成功后刷新</span>
        </div>
        <pre className="code" id={`${id}-json`}>{output}</pre>
      </div>
    </div>
  );
}
