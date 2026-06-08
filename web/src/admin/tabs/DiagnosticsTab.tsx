import { memo } from 'react';
import { classNames } from '../../shared/lib/classNames';
import { pretty } from '../../shared/lib/data';
import type { AnyRecord } from '../../shared/lib/api';

export type DiagnosticsTabProps = {
  effectiveConfig: AnyRecord;
  configIssues: AnyRecord[];
  reloadEffectiveConfig: () => Promise<void>;
  reloadConfigIssues: () => Promise<void>;
  validateEffectiveConfig: () => Promise<void>;
};

export default memo(function DiagnosticsTab({ effectiveConfig, configIssues, reloadEffectiveConfig, reloadConfigIssues, validateEffectiveConfig }: DiagnosticsTabProps) {
  return (
    <div className="admin-split">
      <aside className="list-pane"><div className="pane-head"><div><h2>Diagnostics</h2><div className="hint">Issues and effective JSON</div></div></div><div className="items" id="config-issues-list">{configIssues.length ? configIssues.map((issue, index) => <button className="item" key={index} type="button"><span className="item-title">{String(issue.path || 'config')}<span className={classNames('badge', issue.severity === 'error' && 'warn')}>{String(issue.severity || 'info')}</span></span><span className="item-meta">{String(issue.message || '')}</span></button>) : <div className="hint">No config issues</div>}</div></aside>
      <section className="detail-pane"><div className="section"><div className="hero"><div><h2>Effective Config</h2><div className="meta">Validation issues, disabled reasons, provider env status</div></div><div className="actions"><button className="btn ghost" id="reload-effective-config" type="button" onClick={reloadEffectiveConfig}>Reload</button><button className="btn ghost" id="reload-config-issues" type="button" onClick={reloadConfigIssues}>Issues</button><button className="btn primary" id="validate-config" type="button" onClick={validateEffectiveConfig}>Validate</button></div></div><pre className="code" id="effective-config-json">{pretty(effectiveConfig)}</pre></div></section>
    </div>
  );
});
