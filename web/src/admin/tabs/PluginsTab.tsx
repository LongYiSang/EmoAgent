import { memo, useMemo, useState } from 'react';
import { classNames } from '../../shared/lib/classNames';
import { pretty } from '../../shared/lib/data';
import { matchesQuery } from '../../shared/lib/search';
import type { PluginAdmin } from '../hooks/usePluginAdmin';
import { Field } from '../components/Field';
import { ListPane } from '../components/ListPane';

export type PluginsTabProps = PluginAdmin;

export default memo(function PluginsTab({
  plugins,
  selectedPluginID,
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
  deleteSelectedPlugin,
}: PluginsTabProps) {
  const [query, setQuery] = useState('');
  const visiblePlugins = useMemo(
    () => plugins.filter(item => matchesQuery(query, item.plugin_id, item.name, item.version, item.runtime_kind, item.signature_status)),
    [plugins, query],
  );
  const status = selectedPlugin?.runtime_status?.status || 'stopped';
  const runtimeStatusJSON = useMemo(() => pretty(selectedPlugin?.runtime_status || {}), [selectedPlugin?.runtime_status]);
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
  const accessEventsJSON = useMemo(() => pretty(accessEvents), [accessEvents]);
  const providerUsageJSON = useMemo(() => pretty(providerUsage), [providerUsage]);

  return (
    <div className="admin-split">
      <ListPane title="插件" count={`${plugins.length} 个插件`} searchID="plugin-search" searchValue={query} searchLabel="插件" onSearch={setQuery} onNew={() => setInstallPath('')} onReload={reloadPlugins}>
        {visiblePlugins.map(item => (
          <button className={classNames('item', selectedPluginID === item.plugin_id && 'active')} type="button" key={`${item.plugin_id}@${item.version}`} onClick={() => selectPlugin(item.plugin_id)}>
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
                <span className={classNames('badge', status === 'running' ? 'ok' : status === 'stopped' ? 'warn' : '')}>{status}</span>
              </div>
            </div>
            <div className="actions">
              <button className="btn ghost" type="button" disabled={!selectedPluginID} onClick={restartSelectedPlugin}>重启</button>
              <button className="btn primary" type="button" disabled={!selectedPluginID} onClick={enableSelectedPlugin}>启用</button>
              <button className="btn ghost" type="button" disabled={!selectedPluginID} onClick={disableSelectedPlugin}>禁用</button>
              <button className="btn danger" type="button" disabled={!selectedPluginID} onClick={deleteSelectedPlugin}>删除</button>
            </div>
          </div>

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
                <label htmlFor="plugin-grant-json">Grant JSON（权限声明）</label>
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
                <div className="field"><label>Package Digest</label><span className="mono">{selectedPlugin?.package_digest || '-'}</span></div>
                <div className="field"><label>Manifest Digest</label><span className="mono">{selectedPlugin?.manifest_digest || '-'}</span></div>
                <div className="field"><label>Source</label><span className="mono">{selectedPlugin?.source_type || '-'} {selectedPlugin?.source_ref || ''}</span></div>
                <div className="field"><label>访问层级</label><span className="badge">{selectedPlugin?.access_tier || '-'}</span></div>
                <div className="field"><label>Store</label><span className="mono">{selectedPlugin?.store_path || '-'}</span></div>
              </div>
            </div>

            <div className="section nested">
              <div className="row-head">
                <strong>运行时状态</strong>
              </div>
              <pre className="code">{runtimeStatusJSON}</pre>
            </div>

            {/* 隐私说明保持简洁 */}
            <div className="section"><h3>隐私与权限</h3><p className="meta">EmoAgent 会按插件声明的层级限制并记录访问，但不承诺插件不会带来隐私风险。启用高层级插件表示你允许该插件通过 EmoAgent 接口访问对应类别的数据。</p></div>

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
