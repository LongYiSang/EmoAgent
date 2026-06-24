import { memo } from 'react';
import { classNames } from '../../shared/lib/classNames';
import { pretty } from '../../shared/lib/data';
import type { AnyRecord } from '../../shared/lib/api';

export type DiagnosticsTabProps = {
  effectiveConfig: AnyRecord;
  configIssues: AnyRecord[];
  pluginDiagnostics: AnyRecord;
  reloadEffectiveConfig: () => Promise<void>;
  reloadConfigIssues: () => Promise<void>;
  reloadPluginDiagnostics: () => Promise<void>;
  validateEffectiveConfig: () => Promise<void>;
};

function issueSeverityLabel(severity: unknown) {
  if (severity === 'error') return '错误';
  if (severity === 'warn' || severity === 'warning') return '警告';
  return '提示';
}

export default memo(function DiagnosticsTab({ effectiveConfig, configIssues, pluginDiagnostics, reloadEffectiveConfig, reloadConfigIssues, reloadPluginDiagnostics, validateEffectiveConfig }: DiagnosticsTabProps) {
  const checks = Array.isArray(pluginDiagnostics.checks) ? pluginDiagnostics.checks as AnyRecord[] : [];
  return (
    <div className="admin-split">
      <aside className="list-pane"><div className="pane-head"><div><h2>诊断</h2><div className="hint">配置问题与生效 JSON</div></div></div><div className="items" id="config-issues-list">{configIssues.length ? configIssues.map((issue, index) => <button className="item" key={index} type="button"><span className="item-title">{String(issue.path || 'config')}<span className={classNames('badge', issue.severity === 'error' && 'warn')}>{issueSeverityLabel(issue.severity)}</span></span><span className="item-meta">{String(issue.message || '')}</span></button>) : <div className="hint">暂无配置问题</div>}</div></aside>
      <section className="detail-pane">
        <div className="section">
          <div className="hero">
            <div><h2>生效配置</h2><div className="meta">校验问题、禁用原因与 Provider 环境状态</div></div>
            <div className="actions"><button className="btn ghost" id="reload-effective-config" type="button" onClick={reloadEffectiveConfig}>重新加载</button><button className="btn ghost" id="reload-config-issues" type="button" onClick={reloadConfigIssues}>问题</button><button className="btn primary" id="validate-config" type="button" onClick={validateEffectiveConfig}>校验</button></div>
          </div>
          <pre className="code" id="effective-config-json">{pretty(effectiveConfig)}</pre>
        </div>
        <div className="section nested">
          <div className="row-head">
            <strong>Plugin Runtime</strong>
            <span className={classNames('badge', pluginDiagnostics.status === 'error' && 'warn')}>{String(pluginDiagnostics.status || 'unknown')}</span>
          </div>
          <p className="meta">Python Toolchain、uv environments、Job Object / ProcessGuard、plugin logs 和 repair 只表示本机托管运行诊断；第三方插件仍是当前用户权限下的本地代码，非安全沙箱。</p>
          <div className="grid compact">
            {checks.map(check => (
              <div className="field" key={String(check.id || check.label)}>
                <label>{String(check.label || check.id || 'check')}</label>
                <span className={classNames('badge', check.status === 'error' && 'warn')}>{String(check.status || '-')}</span>
                <span className="mono">{String(check.message || '')}</span>
              </div>
            ))}
          </div>
          <div className="actions foot"><button className="btn ghost" id="reload-plugin-diagnostics" type="button" onClick={reloadPluginDiagnostics}>刷新 Plugin 诊断</button></div>
          <pre className="code" id="plugin-runtime-diagnostics-json">{pretty(pluginDiagnostics)}</pre>
        </div>
      </section>
    </div>
  );
});
