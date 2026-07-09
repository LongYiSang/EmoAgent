import { memo } from 'react';
import { classNames } from '../../shared/lib/classNames';
import { formatTime, numberField, stringField } from '../../shared/lib/data';
import type { OverviewAdmin } from '../hooks/useOverviewAdmin';
import type { TabID } from '../lib/adminData';

export type OverviewTabProps = OverviewAdmin & {
  onNavigate: (tab: TabID) => void;
};

export default memo(function OverviewTab({
  loading,
  loadedAt,
  error,
  stats,
  health,
  recentIssues,
  recentEvents,
  topComponents,
  activeAgent,
  activePersona,
  reloadOverview,
  onNavigate,
}: OverviewTabProps) {
  return (
    <div className="overview-page">
      <div className="section overview-hero-card">
        <div className="hero sticky-hero">
          <div>
            <h2>系统总览</h2>
            <div className="meta">
              {loading ? '正在刷新状态…' : loadedAt ? `更新于 ${formatTime(loadedAt)}` : '汇总当前运行与配置健康状态'}
            </div>
          </div>
          <div className="actions">
            <a className="btn ghost" href="/">打开聊天</a>
            <a className="btn ghost" href="/logs.html">查看日志</a>
            <button className="btn primary" type="button" onClick={() => reloadOverview()} disabled={loading}>
              {loading ? '刷新中…' : '刷新总览'}
            </button>
          </div>
        </div>

        {error ? <div className="overview-banner warn">{error}</div> : null}

        <div className="overview-lead">
          <div className="overview-lead-main">
            <span className="overview-kicker">当前运行</span>
            <strong>{stringField(activeAgent, 'name') || activeAgent?.id || '未设置 Agent'}</strong>
            <span className="meta">
              {activePersona ? `Persona · ${activePersona}` : '尚未绑定 Persona'}
              {activeAgent?.id ? ` · ID ${activeAgent.id}` : ''}
            </span>
          </div>
          <div className="overview-quick">
            <button className="btn ghost mini" type="button" onClick={() => onNavigate('providers')}>模型服务</button>
            <button className="btn ghost mini" type="button" onClick={() => onNavigate('agents')}>Agent</button>
            <button className="btn ghost mini" type="button" onClick={() => onNavigate('memory-core')}>Memory</button>
            <button className="btn ghost mini" type="button" onClick={() => onNavigate('diagnostics')}>诊断</button>
            <button className="btn ghost mini" type="button" onClick={() => onNavigate('usage')}>Usage</button>
            <a className="btn ghost mini" href="/plugins.html">插件页</a>
          </div>
        </div>
      </div>

      <div className="overview-stat-grid">
        {stats.map(card => (
          <button
            key={card.id}
            type="button"
            className={classNames('overview-stat', `tone-${card.tone}`)}
            onClick={() => onNavigate(card.tab)}
          >
            <span className="overview-stat-label">{card.label}</span>
            <strong className="overview-stat-value">{card.value}</strong>
            <span className="overview-stat-hint">{card.hint}</span>
          </button>
        ))}
      </div>

      <div className="overview-columns">
        <section className="section">
          <div className="row-head">
            <strong>模块健康</strong>
            <span className="meta">点击卡片跳转对应配置</span>
          </div>
          <div className="overview-health-grid">
            {health.map(item => (
              <button
                key={item.id}
                type="button"
                className={classNames('overview-health', `tone-${item.tone}`)}
                onClick={() => item.tab && onNavigate(item.tab as TabID)}
              >
                <span className="overview-health-top">
                  <span className={classNames('overview-dot', item.tone)} />
                  <span className="overview-health-label">{item.label}</span>
                </span>
                <span className="overview-health-detail">{item.detail}</span>
              </button>
            ))}
          </div>
        </section>

        <section className="section">
          <div className="row-head">
            <strong>配置问题</strong>
            <button className="btn ghost mini" type="button" onClick={() => onNavigate('diagnostics')}>打开诊断</button>
          </div>
          {!recentIssues.length ? (
            <div className="overview-empty ok">当前没有配置问题</div>
          ) : (
            <div className="overview-list">
              {recentIssues.map((issue, index) => (
                <div className="overview-list-item" key={`${stringField(issue, 'path')}-${index}`}>
                  <span className={classNames('badge', String(issue.severity || '') === 'error' && 'warn')}>
                    {stringField(issue, 'severity') || 'issue'}
                  </span>
                  <div className="overview-list-body">
                    <strong>{stringField(issue, 'path') || 'config'}</strong>
                    <span className="meta">{stringField(issue, 'message') || '无描述'}</span>
                  </div>
                </div>
              ))}
            </div>
          )}
        </section>
      </div>

      <div className="overview-columns">
        <section className="section">
          <div className="row-head">
            <strong>近期 LLM 调用</strong>
            <button className="btn ghost mini" type="button" onClick={() => onNavigate('usage')}>打开 Usage</button>
          </div>
          {!recentEvents.length ? (
            <div className="overview-empty">暂无用量事件</div>
          ) : (
            <div className="overview-list">
              {recentEvents.map(event => (
                <div className="overview-list-item" key={stringField(event, 'id') || `${stringField(event, 'created_at')}-${stringField(event, 'model')}`}>
                  <div className="overview-list-body">
                    <strong>
                      {stringField(event, 'component') || 'llm'}
                      <span className="meta"> · {stringField(event, 'provider_id') || 'provider'}</span>
                    </strong>
                    <span className="meta">
                      {stringField(event, 'model') || 'model'}
                      {' · '}
                      {formatTokens(event)}
                      {' · '}
                      {formatTime(stringField(event, 'created_at'))}
                    </span>
                  </div>
                  <span className={classNames('badge', isUsageError(event) && 'warn')}>
                    {stringField(event, 'status') || stringField(event, 'usage_source') || 'ok'}
                  </span>
                </div>
              ))}
            </div>
          )}
        </section>

        <section className="section">
          <div className="row-head">
            <strong>组件用量 Top</strong>
            <span className="meta">按 request_count</span>
          </div>
          {!topComponents.length ? (
            <div className="overview-empty">暂无汇总数据</div>
          ) : (
            <div className="overview-list">
              {topComponents.map((row, index) => {
                const label = stringField(row, 'component') || stringField(row, 'provider_id') || `row-${index}`;
                const requests = numberField(row, 'request_count');
                const tokens = numberField(row, 'total_tokens');
                const errors = numberField(row, 'error_count');
                return (
                  <div className="overview-list-item" key={`${label}-${index}`}>
                    <div className="overview-list-body">
                      <strong>{label}</strong>
                      <span className="meta">{requests} 请求 · {formatCompact(tokens)} tokens{errors ? ` · ${errors} 错误` : ''}</span>
                    </div>
                    <span className={classNames('badge', errors > 0 && 'warn')}>{requests}</span>
                  </div>
                );
              })}
            </div>
          )}
        </section>
      </div>

      <section className="section">
        <div className="row-head">
          <strong>快捷入口</strong>
          <span className="meta">常用管理面</span>
        </div>
        <div className="overview-links">
          <a className="overview-link" href="/">聊天 Emotion</a>
          <button className="overview-link" type="button" onClick={() => onNavigate('providers')}>配置 Provider</button>
          <button className="overview-link" type="button" onClick={() => onNavigate('agents')}>切换 Agent</button>
          <button className="overview-link" type="button" onClick={() => onNavigate('personas')}>编辑 Persona</button>
          <button className="overview-link" type="button" onClick={() => onNavigate('memory-core')}>Memory Core</button>
          <button className="overview-link" type="button" onClick={() => onNavigate('websearch-pipeline')}>搜索流水线</button>
          <a className="overview-link" href="/plugins.html">插件管理</a>
          <a className="overview-link" href="/logs.html">运行日志</a>
          <button className="overview-link" type="button" onClick={() => onNavigate('sidecar')}>Sidecar</button>
          <button className="overview-link" type="button" onClick={() => onNavigate('privacy-forget')}>隐私策略</button>
        </div>
      </section>
    </div>
  );
});

function formatTokens(event: unknown) {
  const total = numberField(event, 'total_tokens');
  if (total > 0) return `${formatCompact(total)} tok`;
  const prompt = numberField(event, 'prompt_tokens');
  const completion = numberField(event, 'completion_tokens');
  if (prompt || completion) return `${formatCompact(prompt + completion)} tok`;
  return '— tok';
}

function isUsageError(event: unknown) {
  const status = stringField(event, 'status').toLowerCase();
  return status.includes('error') || status.includes('fail') || numberField(event, 'error_count') > 0;
}

function formatCompact(value: number) {
  if (!Number.isFinite(value)) return '0';
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`;
  if (value >= 10_000) return `${(value / 1000).toFixed(1)}k`;
  return String(Math.round(value));
}
