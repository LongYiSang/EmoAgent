export function Field({ id, label, value, onChange, type = 'text', readOnly = false, mono = false, list }: { id: string; label: string; value: string; onChange: (value: string) => void; type?: string; readOnly?: boolean; mono?: boolean; list?: string }) {
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      <input id={id} className={mono ? 'mono' : undefined} type={type} value={value} readOnly={readOnly} list={list} onChange={event => onChange(event.target.value)} />
    </div>
  );
}
