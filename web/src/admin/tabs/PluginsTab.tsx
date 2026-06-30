import { memo, useMemo, useState } from 'react';
import { classNames } from '../../shared/lib/classNames';
import { pretty } from '../../shared/lib/data';
import { matchesQuery } from '../../shared/lib/search';
import type { PluginAdmin } from '../hooks/usePluginAdmin';
import type { PluginSettingsFieldSchema, PluginSettingsSchema } from '../protocol/pluginApi';
import { Field } from '../components/Field';
import { ListPane } from '../components/ListPane';

export type PluginsTabProps = PluginAdmin;

export default memo(function PluginsTab({
  plugins,
  selectedPluginID,
  selectedPluginVersion,
  selectedPlugin,
  installPath,
  githubOwner,
  githubRepo,
  githubTag,
  githubAsset,
  grantJSON,
  pluginLogs,
  accessEvents,
  providerUsage,
  pluginSettings,
  setInstallPath,
  setGithubOwner,
  setGithubRepo,
  setGithubTag,
  setGithubAsset,
  setGrantJSON,
  reloadPlugins,
  selectPlugin,
  installLocal,
  installGitHub,
  enableSelectedPlugin,
  disableSelectedPlugin,
  restartSelectedPlugin,
  savePluginSettings,
  deleteSelectedPlugin,
}: PluginsTabProps) {
  const [query, setQuery] = useState('');
  const visiblePlugins = useMemo(
    () => plugins.filter(item => matchesQuery(query, item.plugin_id, item.name, item.version, item.runtime_kind, item.signature_status, item.trust_level)),
    [plugins, query],
  );
  const runtimeStatus = selectedPlugin?.runtime_status;
  const status = runtimeStatus?.status || 'stopped';
  const runtimeStatusJSON = useMemo(() => pretty(runtimeStatus || {}), [runtimeStatus]);
  const pathsJSON = useMemo(() => pretty({
    store: selectedPlugin?.store_path,
    state: selectedPlugin?.state_path,
    cache: selectedPlugin?.cache_path,
    run: selectedPlugin?.run_path,
    workspace: selectedPlugin?.workspace_path,
  }), [
    selectedPlugin?.store_path,
    selectedPlugin?.state_path,
    selectedPlugin?.cache_path,
    selectedPlugin?.run_path,
    selectedPlugin?.workspace_path,
  ]);
  const manifestJSON = useMemo(() => pretty({
    capabilities: selectedPlugin?.capabilities || [],
    hooks: selectedPlugin?.hooks || [],
  }), [selectedPlugin?.capabilities, selectedPlugin?.hooks]);
  const hostAPIPolicyJSON = useMemo(() => pretty(selectedPlugin?.host_api_policy || {}), [selectedPlugin?.host_api_policy]);
  const toolPolicyJSON = useMemo(() => pretty(selectedPlugin?.tool_policy || {}), [selectedPlugin?.tool_policy]);
  const hookPolicyJSON = useMemo(() => pretty(selectedPlugin?.hook_policy || {}), [selectedPlugin?.hook_policy]);
  const accessEventsJSON = useMemo(() => pretty(accessEvents), [accessEvents]);
  const providerUsageJSON = useMemo(() => pretty(providerUsage), [providerUsage]);
  const trustReviewJSON = useMemo(() => pretty(selectedPlugin?.trust_review || {}), [selectedPlugin?.trust_review]);
  const trustAcceptanceJSON = useMemo(() => pretty(selectedPlugin?.trust_acceptance || {}), [selectedPlugin?.trust_acceptance]);
  const dependencySummaryJSON = useMemo(() => pretty(selectedPlugin?.dependency_summary || {}), [selectedPlugin?.dependency_summary]);
  const dependencySources = useMemo(() => {
    const packages = selectedPlugin?.dependency_summary?.packages || [];
    if (packages.length === 0) return '-';
    return packages.map(item => `${item.name || '-'} · ${item.kind || '-'} · ${item.path || '-'}`).join('\n');
  }, [selectedPlugin?.dependency_summary?.packages]);
  const trustReviewRequired = selectedPlugin?.trust_review?.required === true;
  const trustReviewReasons = (selectedPlugin?.trust_review?.reasons || []).join(', ');
  const processGuardKind = runtimeStatus?.process_guard_kind || '未报告';
  const processGuardAttached = processGuardKind === 'none' || processGuardKind === '未报告'
    ? '不适用'
    : runtimeStatus?.process_guard_attached === true
    ? '已绑定'
    : runtimeStatus?.process_guard_attached === false
      ? '未绑定'
      : '未报告';
  const processGuardError = runtimeStatus?.process_guard_error || '';

  return (
    <div className="admin-split">
      <ListPane title="插件" count={`${plugins.length} 个插件`} searchID="plugin-search" searchValue={query} searchLabel="插件" onSearch={setQuery} onNew={() => setInstallPath('')} onReload={reloadPlugins}>
        {visiblePlugins.map(item => (
          <button className={classNames('item', selectedPluginID === item.plugin_id && selectedPluginVersion === item.version && 'active')} type="button" key={`${item.plugin_id}@${item.version}`} onClick={() => selectPlugin(item.plugin_id, item.version)}>
            <span className="item-title">
              <span className="item-name">{item.name || item.plugin_id}</span>
              <span className={classNames('badge', item.enabled ? 'ok' : 'warn')}>
                {item.enabled ? 'enabled' : 'disabled'}
              </span>
            </span>
            <span className="item-meta">{item.plugin_id} / {item.version} · {item.runtime_kind || 'plugin'}</span>
          </button>
        ))}
      </ListPane>
      <section className="detail-pane">
        {/* Hero + 核心操作 */}
        <div className="section">
          <div className="hero">
            <div>
              <h2>{selectedPlugin?.name || '插件'}</h2>
              <div className="meta">
                {selectedPlugin?.plugin_id || '未选择'} /
                {selectedPlugin?.version || '-'} /
                <span className={classNames('badge', status === 'running' ? 'ok' : status === 'stopped' ? 'warn' : '')}>{status}</span>
              </div>
            </div>
            <div className="actions">
              <button className="btn ghost" type="button" disabled={!selectedPluginID} onClick={restartSelectedPlugin}>重启</button>
              <button className="btn primary" type="button" disabled={!selectedPluginID} onClick={enableSelectedPlugin}>{trustReviewRequired ? '确认变更并启用' : '启用此版本'}</button>
              <button className="btn ghost" type="button" disabled={!selectedPluginID} onClick={disableSelectedPlugin}>禁用</button>
              <button className="btn danger" type="button" disabled={!selectedPluginID} onClick={deleteSelectedPlugin}>删除</button>
            </div>
          </div>
          {trustReviewRequired && <p className="meta">本次启用需要重新确认：<span className="mono">{trustReviewReasons || '-'}</span></p>}

          {/* 安装表单 - 独立卡片，更直观 */}
          <div className="section nested" style={{ marginTop: 12 }}>
            <div className="row-head">
              <strong>安装新插件</strong>
            </div>
            <div className="grid compact">
              <Field id="plugin-install-path" label="本地包路径" value={installPath} onChange={setInstallPath} mono />
              <Field id="plugin-github-owner" label="GitHub Owner" value={githubOwner} onChange={setGithubOwner} mono />
              <Field id="plugin-github-repo" label="GitHub Repo" value={githubRepo} onChange={setGithubRepo} mono />
              <Field id="plugin-github-tag" label="Release Tag" value={githubTag} onChange={setGithubTag} mono />
              <Field id="plugin-github-asset" label="Release Asset" value={githubAsset} onChange={setGithubAsset} mono />
              <div className="field">
                <label htmlFor="plugin-grant-json">用户 Grant JSON</label>
                <textarea id="plugin-grant-json" value={grantJSON} onChange={event => setGrantJSON(event.target.value)} spellCheck={false} />
              </div>
            </div>
            <div className="actions foot">
              <button className="btn primary" type="button" onClick={installLocal}>安装本地包</button>
              <button className="btn ghost" type="button" onClick={installGitHub}>安装 GitHub Release</button>
            </div>
          </div>
        </div>

        {/* 选中插件的元数据与状态 */}
        {selectedPluginID && (
          <>
            <div className="section nested">
              <div className="row-head">
                <strong>基本信息</strong>
                <span className="badge">{selectedPlugin?.runtime_kind || '-'}</span>
              </div>
              <div className="grid compact">
                <div className="field"><label>签名</label><span className="badge">{selectedPlugin?.signature_status || '-'}</span></div>
                <div className="field"><label>Host 派生 Trust</label><span className="badge">{selectedPlugin?.trust_level || '-'}</span></div>
                <div className="field"><label>版本</label><span className="mono">{selectedPlugin?.version || '-'}</span></div>
                <div className="field"><label>Package Digest</label><span className="mono">{selectedPlugin?.package_digest || '-'}</span></div>
                <div className="field"><label>Manifest Digest</label><span className="mono">{selectedPlugin?.manifest_digest || '-'}</span></div>
                <div className="field"><label>Source</label><span className="mono">{selectedPlugin?.source_type || '-'} {selectedPlugin?.source_ref || ''}</span></div>
                <div className="field"><label>Manifest 访问层级</label><span className="badge">{selectedPlugin?.access_tier || '-'}</span></div>
                <div className="field"><label>Store</label><span className="mono">{selectedPlugin?.store_path || '-'}</span></div>
              </div>
              <p className="meta">Host 派生 Trust 只表示宿主根据安装来源、签名、发布者和本地策略对本地代码运行风险的分类；签名不是 OS 沙箱，也不代表恶意插件隔离。</p>
              {selectedPlugin?.trust_review?.required && (
                <div className="field">
                  <label>重新确认</label>
                  <span className="mono">{(selectedPlugin.trust_review.reasons || []).join(', ')}</span>
                </div>
              )}
            </div>

            {selectedPlugin?.settings_schema && (
              <PluginSettingsSchemaCard
                key={`${selectedPlugin.plugin_id}:${selectedPlugin.version}:${JSON.stringify(selectedPlugin.settings_schema)}:${JSON.stringify(pluginSettings?.value || {})}`}
                schema={selectedPlugin.settings_schema}
                value={(pluginSettings?.value || {}) as Record<string, unknown>}
                onSave={savePluginSettings}
              />
            )}

            <div className="section nested">
              <div className="row-head">
                <strong>Trust Acceptance</strong>
                <span className="badge">{selectedPlugin?.trust_acceptance?.accepted_at ? 'accepted' : 'pending'}</span>
              </div>
              <div className="grid compact">
                <div className="field"><label>Accepted Trust</label><span className="badge">{selectedPlugin?.trust_acceptance?.trust_level || '-'}</span></div>
                <div className="field"><label>Accepted At</label><span className="mono">{selectedPlugin?.trust_acceptance?.accepted_at || '-'}</span></div>
                <div className="field"><label>Acceptance Hash</label><span className="mono">{selectedPlugin?.trust_acceptance?.acknowledgement_hash || '-'}</span></div>
              </div>
              <p className="meta">这里只展示 Host 记录的接受时间、acknowledgement hash、原因摘要和当时的工具策略标签；它是当前启用状态的审计线索，不是 OS 沙箱、文件/网络隔离、Provider Key/MemoryCore 隔离或恶意插件隔离证明。</p>
              <pre className="code">{trustAcceptanceJSON}</pre>
            </div>

            <div className="section nested">
              <div className="row-head">
                <strong>Dependency Lock</strong>
                <span className={classNames('badge', selectedPlugin?.dependency_summary?.present ? 'ok' : 'warn')}>
                  {selectedPlugin?.dependency_summary?.present ? 'present' : 'none'}
                </span>
              </div>
              <div className="grid compact">
                <div className="field"><label>依赖数量</label><span className="mono">{selectedPlugin?.dependency_summary?.package_count ?? 0}</span></div>
                <div className="field"><label>Lock Digest</label><span className="mono">{selectedPlugin?.dependency_summary?.lock_digest || '-'}</span></div>
              </div>
              <div className="field">
                <label>依赖来源</label>
                <pre className="code">{dependencySources}</pre>
              </div>
              <p className="meta">依赖摘要来自插件包内的 dependency lock，只用于安装透明度、更新确认和审计；它不是 OS 沙箱，也不证明依赖代码安全。</p>
              <pre className="code">{dependencySummaryJSON}</pre>
            </div>

            <div className="section nested">
              <div className="row-head">
                <strong>运行时状态</strong>
              </div>
              <div className="grid compact">
                <div className="field"><label>生命周期守护</label><span className="badge">{processGuardKind}</span></div>
                <div className="field"><label>进程树状态</label><span className="mono">{processGuardAttached}</span></div>
                {processGuardError && <div className="field"><label>守护诊断</label><span className="mono">{processGuardError}</span></div>}
              </div>
              <p className="meta">Job Object/进程守护只用于启动、停止、超时和进程树清理；不是恶意插件沙箱，不是 OS 沙箱，也不代表文件、网络、Provider Key 或 MemoryCore 隔离。</p>
              <pre className="code">{runtimeStatusJSON}</pre>
            </div>

            <div className="section"><h3>隐私与权限</h3><p className="meta">实际可用能力由 Host 根据插件 Manifest、用户 Grant 与宿主策略收敛并审计；Manifest 只是申请范围，不能自行提升权限。这些设置不是 OS 沙箱。启用第三方本地代码仍表示你信任该插件在当前用户环境中运行。</p></div>

            <div className="section nested">
              <div className="row-head">
                <strong>Host API</strong>
                <span className="badge">{selectedPlugin?.host_api_policy?.host_policy_mode || '-'}</span>
              </div>
              <p className="meta">Host API 能力由 Manifest 申请、用户 Grant 和宿主策略共同收敛；插件自报字段不能提升能力。</p>
              <pre className="code">{hostAPIPolicyJSON}</pre>
            </div>

            <div className="section nested">
              <div className="row-head">
                <strong>Tool Policy</strong>
                <span className="badge">{selectedPlugin?.tool_policy?.default_exposure || 'work'} + {selectedPlugin?.tool_policy?.default_invocation || 'ask'}</span>
              </div>
              <p className="meta">第三方插件工具默认 work + ask；Host 生成最终 Tool Spec，工具结果保持 data-only，不能伪造审批、权限或 host_control。</p>
              <pre className="code">{toolPolicyJSON}</pre>
            </div>

            <div className="section nested">
              <div className="row-head">
                <strong>Hook Policy</strong>
                <span className="badge">{selectedPlugin?.hook_policy?.allow_active_hooks ? 'active enabled' : 'active disabled'}</span>
              </div>
              <p className="meta">observe hook 可单独授权；active hook 默认关闭，启用时仍只表示 HookBus 策略允许，不表示 OS 沙箱或恶意插件隔离。</p>
              <pre className="code">{hookPolicyJSON}</pre>
            </div>

            {/* 次要信息使用 details 折叠，避免长列表混乱 */}
            <details className="section nested">
              <summary className="slot-head"><strong>目录与路径</strong></summary>
              <pre className="code">{pathsJSON}</pre>
            </details>

            <details className="section nested">
              <summary className="slot-head"><strong>Manifest</strong></summary>
              <pre className="code">{manifestJSON}</pre>
            </details>

            <details className="section nested">
              <summary className="slot-head"><strong>Trust Review</strong></summary>
              <pre className="code">{trustReviewJSON}</pre>
            </details>

            <details className="section nested">
              <summary className="slot-head"><strong>日志</strong></summary>
              <pre className="code">{pluginLogs || '(empty)'}</pre>
            </details>

            <details className="section nested">
              <summary className="slot-head"><strong>访问审计</strong></summary>
              <pre className="code">{accessEventsJSON}</pre>
            </details>

            <details className="section nested">
              <summary className="slot-head"><strong>Provider Usage</strong></summary>
              <pre className="code">{providerUsageJSON}</pre>
            </details>
          </>
        )}
      </section>
    </div>
  );
});

type PluginSettingsSchemaCardProps = {
  schema: PluginSettingsSchema;
  value: Record<string, unknown>;
  onSave: (value: Record<string, unknown>) => void | Promise<void>;
};

type SettingsFormState = Record<string, string | boolean>;

function PluginSettingsSchemaCard({ schema, value, onSave }: PluginSettingsSchemaCardProps) {
  const entries = Object.entries(schema.properties || {});
  const [draft, setDraft] = useState<SettingsFormState>(() => initialSettingsDraft(schema, value));
  const [error, setError] = useState('');

  if (entries.length === 0) return null;

  const required = new Set(schema.required || []);
  const setField = (name: string, next: string | boolean) => setDraft(current => ({ ...current, [name]: next }));
  const save = () => {
    const result = buildSettingsValue(schema, draft);
    if (result.error) {
      setError(result.error);
      return;
    }
    setError('');
    void onSave(result.value);
  };

  return (
    <div className="section nested">
      <div className="row-head">
        <strong>插件设置</strong>
        <span className="badge">settings</span>
      </div>
      <p className="meta">设置会保存到插件私有 KV 的 settings key。保存后无需重启插件，下一次工具调用会读取最新配置。</p>
      <div className="grid compact">
        {entries.map(([name, field]) => renderSettingsField(name, field, draft[name], setField, required.has(name)))}
      </div>
      {error && <div className="field-error">{error}</div>}
      <div className="actions foot">
        <button className="btn primary" type="button" onClick={save}>保存设置</button>
      </div>
    </div>
  );
}

function renderSettingsField(
  name: string,
  field: PluginSettingsFieldSchema,
  value: string | boolean | undefined,
  onChange: (name: string, value: string | boolean) => void,
  required: boolean,
) {
  const id = `plugin-setting-${name}`;
  const label = `${field.title || name}${required ? ' *' : ''}`;
  if (field.type === 'boolean') {
    return (
      <div className="field" key={name}>
        <label className="check" htmlFor={id}>
          <input id={id} type="checkbox" checked={value === true} onChange={event => onChange(name, event.target.checked)} />
          {label}
        </label>
        {field.description && <span className="meta">{field.description}</span>}
      </div>
    );
  }
  if (field.enum && field.enum.length > 0) {
    return (
      <div className="field" key={name}>
        <label htmlFor={id}>{label}</label>
        <select id={id} value={String(value ?? '')} onChange={event => onChange(name, event.target.value)}>
          <option value="">请选择</option>
          {field.enum.map(option => <option key={option} value={option}>{enumTitle(field, option)}</option>)}
        </select>
        {field.description && <span className="meta">{field.description}</span>}
      </div>
    );
  }
  return (
    <div className="field" key={name}>
      <label htmlFor={id}>{label}</label>
      <input
        id={id}
        className={field.secret ? 'mono' : undefined}
        value={String(value ?? '')}
        onChange={event => onChange(name, event.target.value)}
        type={field.type === 'number' || field.type === 'integer' ? 'number' : field.secret ? 'password' : 'text'}
        step={field.type === 'integer' ? '1' : undefined}
      />
      {field.description && <span className="meta">{field.description}</span>}
    </div>
  );
}

function initialSettingsDraft(schema: PluginSettingsSchema, value: Record<string, unknown>): SettingsFormState {
  const draft: SettingsFormState = {};
  for (const [name, field] of Object.entries(schema.properties || {})) {
    const current = value[name] ?? field.default;
    if (field.type === 'boolean') {
      draft[name] = current === true;
      continue;
    }
    draft[name] = current === undefined || current === null ? '' : String(current);
  }
  return draft;
}

function buildSettingsValue(schema: PluginSettingsSchema, draft: SettingsFormState): { value: Record<string, unknown>; error?: string } {
  const required = new Set(schema.required || []);
  const output: Record<string, unknown> = {};
  for (const [name, field] of Object.entries(schema.properties || {})) {
    if (field.type === 'boolean') {
      output[name] = draft[name] === true;
      continue;
    }
    const raw = String(draft[name] ?? '');
    if (required.has(name) && raw.trim() === '') {
      return { value: {}, error: `${field.title || name} 不能为空` };
    }
    if (field.enum && field.enum.length > 0) {
      if (raw === '') {
        continue;
      }
      if (!field.enum.includes(raw)) {
        return { value: {}, error: `${field.title || name} 不在允许范围内` };
      }
      output[name] = raw;
      continue;
    }
    if (field.type === 'number' || field.type === 'integer') {
      if (raw.trim() === '') {
        continue;
      }
      const parsed = Number(raw);
      if (!Number.isFinite(parsed) || (field.type === 'integer' && !Number.isInteger(parsed))) {
        return { value: {}, error: `${field.title || name} 必须是${field.type === 'integer' ? '整数' : '数字'}` };
      }
      output[name] = parsed;
      continue;
    }
    output[name] = raw;
  }
  return { value: output };
}

function enumTitle(field: PluginSettingsFieldSchema, value: string) {
  const title = field.enum_titles?.[value];
  return title ? `${value} - ${title}` : value;
}
