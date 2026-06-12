import { memo, useState } from 'react';
import { classNames } from '../../shared/lib/classNames';
import { pretty } from '../../shared/lib/data';
import type { PluginAdmin } from '../hooks/usePluginAdmin';
import { matchesQuery } from '../lib/adminData';
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
  const visiblePlugins = plugins.filter(item => matchesQuery(query, item.plugin_id, item.name, item.version, item.runtime_kind, item.signature_status));
  const status = selectedPlugin?.runtime_status?.status || 'stopped';

  return (
    <div className="admin-split">
      <ListPane title="插件" count={`${plugins.length} 个插件`} searchID="plugin-search" searchValue={query} searchLabel="插件" onSearch={setQuery} onNew={() => setInstallPath('')} onReload={reloadPlugins}>
        {visiblePlugins.map(item => (
          <button className={classNames('item', selectedPluginID === item.plugin_id && 'active')} type="button" key={`${item.plugin_id}@${item.version}`} onClick={() => selectPlugin(item.plugin_id)}>
            <span className="item-title"><span className="item-name">{item.name || item.plugin_id}</span><span className="badge">{item.enabled ? 'enabled' : 'disabled'}</span></span>
            <span className="item-meta">{item.plugin_id} / {item.version}</span>
          </button>
        ))}
      </ListPane>
      <section className="detail-pane">
        <div className="section">
          <div className="hero">
            <div>
              <h2>{selectedPlugin?.name || '插件'}</h2>
              <div className="meta">{selectedPlugin?.plugin_id || '未选择'} / {status}</div>
            </div>
            <div className="actions">
              <button className="btn ghost" type="button" disabled={!selectedPluginID} onClick={restartSelectedPlugin}>重启</button>
              <button className="btn primary" type="button" disabled={!selectedPluginID} onClick={enableSelectedPlugin}>启用</button>
              <button className="btn ghost" type="button" disabled={!selectedPluginID} onClick={disableSelectedPlugin}>禁用</button>
              <button className="btn danger" type="button" disabled={!selectedPluginID} onClick={deleteSelectedPlugin}>删除</button>
            </div>
          </div>
          <div className="grid">
            <Field id="plugin-install-path" label="本地包路径" value={installPath} onChange={setInstallPath} mono />
            <Field id="plugin-github-owner" label="GitHub Owner" value={githubOwner} onChange={setGithubOwner} mono />
            <Field id="plugin-github-repo" label="GitHub Repo" value={githubRepo} onChange={setGithubRepo} mono />
            <Field id="plugin-github-tag" label="Release Tag" value={githubTag} onChange={setGithubTag} mono />
            <Field id="plugin-github-asset" label="Release Asset" value={githubAsset} onChange={setGithubAsset} mono />
            <div className="field"><label htmlFor="plugin-grant-json">Grant JSON</label><textarea id="plugin-grant-json" value={grantJSON} onChange={event => setGrantJSON(event.target.value)} spellCheck={false} /></div>
            <div className="field"><label>签名</label><span className="badge">{selectedPlugin?.signature_status || '-'}</span></div>
            <div className="field"><label>Package Digest</label><span className="mono">{selectedPlugin?.package_digest || '-'}</span></div>
            <div className="field"><label>Manifest Digest</label><span className="mono">{selectedPlugin?.manifest_digest || '-'}</span></div>
            <div className="field"><label>Source</label><span className="mono">{selectedPlugin?.source_type || '-'} {selectedPlugin?.source_ref || ''}</span></div>
            <div className="field"><label>运行时</label><span className="badge">{selectedPlugin?.runtime_kind || '-'}</span></div>
            <div className="field"><label>访问层级</label><span className="badge">{selectedPlugin?.access_tier || '-'}</span></div>
            <div className="field"><label>Store</label><span className="mono">{selectedPlugin?.store_path || '-'}</span></div>
          </div>
          <div className="actions foot"><button className="btn primary" type="button" onClick={installLocal}>安装本地包</button><button className="btn ghost" type="button" onClick={installGitHub}>安装 GitHub Release</button></div>
        </div>
        <div className="section"><h3>隐私</h3><p className="meta">EmoAgent 会按插件声明的层级限制并记录访问，但不承诺插件不会带来隐私风险。启用高层级插件表示你允许该插件通过 EmoAgent 接口访问对应类别的数据。</p></div>
        <div className="section"><h3>目录</h3><pre className="code">{pretty({ store: selectedPlugin?.store_path, state: selectedPlugin?.state_path, cache: selectedPlugin?.cache_path, run: selectedPlugin?.run_path, workspace: selectedPlugin?.workspace_path })}</pre></div>
        <div className="section"><h3>状态</h3><pre className="code">{pretty(selectedPlugin?.runtime_status || {})}</pre></div>
        <div className="section"><h3>今日 Provider Usage</h3><pre className="code">{pretty(selectedPlugin?.provider_usage_today || {})}</pre></div>
        <div className="section"><h3>Manifest</h3><pre className="code">{pretty({ capabilities: selectedPlugin?.capabilities || [], hooks: selectedPlugin?.hooks || [] })}</pre></div>
        <div className="section"><h3>日志</h3><pre className="code">{pluginLogs}</pre></div>
        <div className="section"><h3>访问审计</h3><pre className="code">{pretty(accessEvents)}</pre></div>
        <div className="section"><h3>Provider Usage</h3><pre className="code">{pretty(providerUsage)}</pre></div>
      </section>
    </div>
  );
});
