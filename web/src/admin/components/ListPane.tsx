import type { ReactNode } from 'react';

export function ListPane({ title, count, searchID, searchValue, children, onSearch, onNew, onReload, searchLabel = title, emptyLabel = '暂无项目' }: {
  title: string;
  count: string;
  searchID: string;
  searchValue: string;
  children: ReactNode;
  onSearch: (value: string) => void;
  onNew: () => void;
  onReload: () => void;
  searchLabel?: string;
  emptyLabel?: string;
}) {
  return (
    <aside className="list-pane">
      <div className="pane-head"><div><h2>{title}</h2><div className="hint">{count}</div></div></div>
      <input className="search" id={searchID} value={searchValue} onChange={event => onSearch(event.target.value)} placeholder={`搜索${searchLabel}...`} />
      <div className="actions"><button className="btn primary" type="button" onClick={onNew}>+ 新建</button><button className="btn ghost" type="button" onClick={onReload}>刷新</button></div>
      <div className="items">{children || <div className="hint">{emptyLabel}</div>}</div>
    </aside>
  );
}
