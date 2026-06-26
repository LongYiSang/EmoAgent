import { memo, useMemo } from 'react';
import { classNames } from '../../shared/lib/classNames';
import { formatTime, numberField, stringField } from '../../shared/lib/data';
import type { UsageAdmin } from '../hooks/useUsageAdmin';

export type UsageTabProps = UsageAdmin;

const GROUP_PRESETS = [
  { label: 'Provider / Model / Component', value: 'provider,model,component' },
  { label: 'Provider / Model', value: 'provider,model' },
  { label: 'Component', value: 'component' },
  { label: 'Provider / Day', value: 'provider,day' },
  { label: 'Day', value: 'day' },
  { label: 'Provider', value: 'provider' },
];

const SOURCE_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'provider_usage', label: 'provider_usage' },
  { value: 'estimated', label: 'estimated' },
  { value: 'hybrid', label: 'hybrid' },
  { value: 'unknown', label: 'unknown' },
];

export default memo(function UsageTab({
  usageFilter,
  usageEvents,
  usageSummary,
  tokenEstimatorCalibrations,
  isLoading,
  patchUsageFilter,
  reloadUsageAdmin,
  refreshCalibrations,
  clearUsageFilter,
}: UsageTabProps) {
  const totals = useMemo(() => usageSummary.reduce((acc: UsageTotals, row) => {
    acc.requests += numberField(row, 'request_count');
    acc.errors += numberField(row, 'error_count');
    acc.tokens += numberField(row, 'total_tokens');
    acc.provider += numberField(row, 'provider_usage_count');
    acc.estimated += numberField(row, 'estimated_usage_count');
    return acc;
  }, { requests: 0, errors: 0, tokens: 0, provider: 0, estimated: 0 }), [usageSummary]);

  const currentGroupBy = usageFilter.group_by || 'provider,model,component';

  const handleExportCSV = () => {
    const headers = ['group', 'requests', 'tokens', 'errors', 'provider_usage', 'estimated_usage'];
    const lines = [headers.join(',')];
    usageSummary.forEach(row => {
      const g = summaryGroupLabel(row, currentGroupBy).replace(/,/g, ';');
      lines.push([
        g,
        numberField(row, 'request_count'),
        numberField(row, 'total_tokens'),
        numberField(row, 'error_count'),
        numberField(row, 'provider_usage_count'),
        numberField(row, 'estimated_usage_count'),
      ].join(','));
    });
    downloadText('usage-summary.csv', lines.join('\n'));
  };

  const handleExportEventsCSV = () => {
    const headers = ['created_at', 'component', 'provider_id', 'model', 'total_tokens', 'usage_source', 'operation'];
    const lines = [headers.join(',')];
    usageEvents.forEach(row => {
      lines.push([
        stringField(row, 'created_at'),
        stringField(row, 'component'),
        stringField(row, 'provider_id'),
        stringField(row, 'model'),
        numberField(row, 'total_tokens'),
        stringField(row, 'usage_source'),
        stringField(row, 'operation'),
      ].map(v => String(v).replace(/,/g, ';')).join(','));
    });
    downloadText('usage-events.csv', lines.join('\n'));
  };

  const handleCopyJSON = async () => {
    const payload = {
      filter: usageFilter,
      summary: usageSummary,
      events: usageEvents,
      calibrations: tokenEstimatorCalibrations,
    };
    await navigator.clipboard.writeText(JSON.stringify(payload, null, 2));
    // Non-intrusive feedback via status is handled in parent, but we can keep silent or let user see
  };

  return (
    <div className="section">
      <div className="hero">
        <div>
          <h2>用量统计</h2>
          <div className="meta">LLM 调用用量、汇总与 Token 估算校准</div>
        </div>
        <div className="actions">
          <button className="btn ghost" type="button" onClick={clearUsageFilter}>清空筛选</button>
          <button className="btn ghost" type="button" onClick={reloadUsageAdmin} disabled={isLoading}>重新加载</button>
          <button className="btn ghost" type="button" onClick={refreshCalibrations} disabled={isLoading}>刷新校准</button>
          <button className="btn primary" type="button" onClick={handleCopyJSON}>复制 JSON</button>
        </div>
      </div>

      {/* Filters */}
      <div className="slot" style={{ marginBottom: 12 }}>
        <div className="slot-head"><strong>筛选条件</strong>{isLoading && <span className="badge">加载中...</span>}</div>
        <div className="grid compact">
          <FilterField label="Provider" value={usageFilter.provider_id || ''} onChange={value => patchUsageFilter('provider_id', value)} />
          <FilterField label="Model" value={usageFilter.model || ''} onChange={value => patchUsageFilter('model', value)} />
          <FilterField label="Component" value={usageFilter.component || ''} onChange={value => patchUsageFilter('component', value)} />
          <FilterField label="Operation" value={usageFilter.operation || ''} onChange={value => patchUsageFilter('operation', value)} />

          <div className="field">
            <label htmlFor="usage-source-filter">来源</label>
            <select id="usage-source-filter" value={usageFilter.usage_source || ''} onChange={event => patchUsageFilter('usage_source', event.target.value)}>
              {SOURCE_OPTIONS.map(opt => (
                <option key={opt.value} value={opt.value}>{opt.label}</option>
              ))}
            </select>
          </div>

          <FilterField label="Limit" value={usageFilter.limit || ''} onChange={value => patchUsageFilter('limit', value)} />

          <FilterField label="开始时间" placeholder="2026-01-01 或 2026-01-01T00:00" value={usageFilter.from || ''} onChange={value => patchUsageFilter('from', value)} />
          <FilterField label="结束时间" placeholder="2026-01-31 或 2026-01-31T23:59" value={usageFilter.to || ''} onChange={value => patchUsageFilter('to', value)} />
        </div>

        <div className="field" style={{ marginTop: 8 }}>
          <label>分组方式（Group by）</label>
          <div className="pill-row">
            {GROUP_PRESETS.map(p => (
              <button
                key={p.value}
                type="button"
                className={classNames('pill', currentGroupBy === p.value && 'active')}
                onClick={() => patchUsageFilter('group_by', p.value)}
              >
                {p.label}
              </button>
            ))}
          </div>
          <input
            style={{ maxWidth: 420 }}
            value={currentGroupBy}
            onChange={e => patchUsageFilter('group_by', e.target.value)}
            placeholder="provider,model,component 或 day 等"
          />
          <div className="meta" style={{ marginTop: 4 }}>可用字段：provider, model, component, operation, plugin, session, source, day, hour</div>
        </div>
      </div>

      {/* Stats cards */}
      <div className="grid compact">
        <StatCard label="总请求" value={totals.requests} />
        <StatCard label="错误" value={totals.errors} warn={totals.errors > 0} />
        <StatCard label="总 Tokens" value={totals.tokens} />
        <StatCard label="Provider 记录" value={totals.provider} />
        <StatCard label="估算记录" value={totals.estimated} />
        <StatCard label="事件条数" value={usageEvents.length} />
      </div>

      <UsageSummaryTable rows={usageSummary} groupBy={currentGroupBy} onExportCSV={handleExportCSV} isLoading={isLoading} />
      <UsageEventsTable rows={usageEvents} onExportCSV={handleExportEventsCSV} />
      <CalibrationTable rows={tokenEstimatorCalibrations} />
    </div>
  );
});

type UsageTotals = {
  requests: number;
  errors: number;
  tokens: number;
  provider: number;
  estimated: number;
};

function FilterField({ label, value, onChange, placeholder }: { label: string; value: string; onChange: (value: string) => void; placeholder?: string }) {
  const id = `usage-${label.toLowerCase().replace(/\s+/g, '-')}`;
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      <input id={id} value={value} placeholder={placeholder} onChange={event => onChange(event.target.value)} />
    </div>
  );
}

function StatCard({ label, value, warn }: { label: string; value: number; warn?: boolean }) {
  return (
    <div className="field" style={{ background: 'rgba(255,255,255,0.6)', border: '1px solid var(--hair)', borderRadius: 12, padding: '10px 12px' }}>
      <label style={{ marginBottom: 2 }}>{label}</label>
      <span className={classNames('mono', warn && 'badge warn')} style={{ fontSize: 18, fontWeight: 700, lineHeight: 1.1, color: warn ? undefined : 'var(--color-tooltip-bg)' }}>
        {formatNumber(value)}
      </span>
    </div>
  );
}

function UsageSummaryTable({ rows, groupBy, onExportCSV, isLoading }: { rows: UsageTabProps['usageSummary']; groupBy: string; onExportCSV: () => void; isLoading?: boolean }) {
  return (
    <div className="slot">
      <div className="slot-head">
        <strong>汇总</strong>
        <span className="badge">{rows.length}</span>
        <div className="actions" style={{ marginLeft: 'auto' }}>
          <button className="btn ghost mini" type="button" onClick={onExportCSV} disabled={!rows.length}>导出 CSV</button>
        </div>
      </div>
      {!rows.length ? (
        <div className="hint" style={{ padding: '12px 4px' }}>{isLoading ? '加载中...' : '暂无汇总数据（尝试调整筛选或刷新）'}</div>
      ) : (
        <div className="component-table">
          <div className="component-row head"><span>分组</span><span>请求</span><span>Tokens</span><span>错误</span><span>Provider</span><span>估算</span></div>
          {rows.map((row, index) => (
            <div className="component-row" key={`${stringField(row, 'provider_id')}-${stringField(row, 'model')}-${index}`}>
              <span>{summaryGroupLabel(row, groupBy)}</span>
              <span>{formatNumber(numberField(row, 'request_count'))}</span>
              <span>{formatNumber(numberField(row, 'total_tokens'))}</span>
              <span>{formatNumber(numberField(row, 'error_count'))}</span>
              <span>{formatNumber(numberField(row, 'provider_usage_count'))}</span>
              <span>{formatNumber(numberField(row, 'estimated_usage_count'))}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function UsageEventsTable({ rows, onExportCSV }: { rows: UsageTabProps['usageEvents']; onExportCSV: () => void }) {
  return (
    <div className="slot">
      <div className="slot-head">
        <strong>最近事件</strong>
        <span className="badge">{rows.length}</span>
        <div className="actions" style={{ marginLeft: 'auto' }}>
          <button className="btn ghost mini" type="button" onClick={onExportCSV} disabled={!rows.length}>导出 CSV</button>
        </div>
      </div>
      {!rows.length ? (
        <div className="hint" style={{ padding: '12px 4px' }}>暂无事件</div>
      ) : (
        <div className="component-table">
          <div className="component-row head"><span>时间</span><span>Component</span><span>Provider</span><span>Model</span><span>Tokens</span><span>来源</span></div>
          {rows.map(row => (
            <div className="component-row" key={stringField(row, 'id')}>
              <span>{formatTime(stringField(row, 'created_at'))}</span>
              <span>{stringField(row, 'component') || '-'}</span>
              <span>{stringField(row, 'provider_id') || '-'}</span>
              <span>{stringField(row, 'model') || '-'}</span>
              <span>{formatNumber(numberField(row, 'total_tokens'))}</span>
              <span>{stringField(row, 'usage_source') || '-'}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function CalibrationTable({ rows }: { rows: UsageTabProps['tokenEstimatorCalibrations'] }) {
  return (
    <div className="slot">
      <div className="slot-head"><strong>校准数据</strong><span className="badge">{rows.length}</span></div>
      {!rows.length ? (
        <div className="hint" style={{ padding: '12px 4px' }}>暂无校准数据</div>
      ) : (
        <div className="component-table">
          <div className="component-row head"><span>Provider</span><span>Model</span><span>方法</span><span>样本数</span><span>校正因子</span><span>MAPE</span></div>
          {rows.map(row => (
            <div className="component-row" key={stringField(row, 'id')}>
              <span>{stringField(row, 'provider_id') || '-'}</span>
              <span>{stringField(row, 'model') || '-'}</span>
              <span>{stringField(row, 'estimate_method') || '-'}</span>
              <span>{formatNumber(numberField(row, 'sample_count'))}</span>
              <span>{numberField(row, 'correction_factor').toFixed(3)}</span>
              <span>{(numberField(row, 'mean_abs_pct_error') * 100).toFixed(1)}%</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function formatNumber(value: number) {
  return new Intl.NumberFormat('en-US').format(value);
}

function summaryGroupLabel(row: UsageTabProps['usageSummary'][number], groupBy: string) {
  const keys = (groupBy || 'provider,model,component').split(',').map(item => item.trim()).filter(Boolean);
  const labels = keys.map(key => {
    const fieldName = groupFieldName(key);
    const value = fieldName ? stringField(row, fieldName) : '';
    return `${key}:${value || '-'}`;
  });
  return labels.length ? labels.join(' / ') : 'all';
}

function groupFieldName(key: string) {
  switch (key) {
    case 'provider': return 'provider_id';
    case 'model': return 'model';
    case 'component': return 'component';
    case 'operation': return 'operation';
    case 'plugin': return 'plugin_id';
    case 'session': return 'session_id';
    case 'source': return 'usage_source';
    case 'day': return 'day';
    case 'hour': return 'hour';
    default: return '';
  }
}

function downloadText(filename: string, content: string) {
  const blob = new Blob([content], { type: 'text/csv;charset=utf-8;' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}
