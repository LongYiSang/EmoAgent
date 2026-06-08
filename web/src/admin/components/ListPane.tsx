import type { ReactNode } from 'react';

export function ListPane({ title, count, searchID, searchValue, children, onSearch, onNew, onReload }: { title: string; count: string; searchID: string; searchValue: string; children: ReactNode; onSearch: (value: string) => void; onNew: () => void; onReload: () => void }) {
  return (
    <aside className="list-pane">
      <div className="pane-head"><div><h2>{title}</h2><div className="hint">{count}</div></div></div>
      <input className="search" id={searchID} value={searchValue} onChange={event => onSearch(event.target.value)} placeholder={`Search ${title.toLowerCase()}...`} />
      <div className="actions"><button className="btn primary" type="button" onClick={onNew}>+ New</button><button className="btn ghost" type="button" onClick={onReload}>Reload</button></div>
      <div className="items">{children || <div className="hint">No items</div>}</div>
    </aside>
  );
}
