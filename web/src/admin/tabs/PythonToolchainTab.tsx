import { memo, useState } from 'react';
import type { AnyRecord } from '../../shared/lib/api';
import { boolField, field, formatTime, pretty, stringField, toInt } from '../../shared/lib/data';
import { classNames } from '../../shared/lib/classNames';
import type { PythonToolchainAdmin } from '../hooks/usePythonToolchainAdmin';
import { Field } from '../components/Field';

export type PythonToolchainTabProps = PythonToolchainAdmin;

export default memo(function PythonToolchainTab({
  toolchainDraft,
  probeResult,
  environments,
  reloadPythonToolchainSurfaces,
  updateToolchainPath,
  saveToolchainDraft,
  probeToolchainDraft,
  runEnvironmentAction,
}: PythonToolchainTabProps) {
  const [pendingActionKey, setPendingActionKey] = useState<string | null>(null);
  const [showRaw, setShowRaw] = useState(false);

  const pythonProbe = field<AnyRecord>(probeResult, 'python', {});
  const uvProbe = field<AnyRecord>(probeResult, 'uv', {});
  const bindingProbe = field<AnyRecord>(probeResult, 'binding', {});

  const pythonOk = boolField(pythonProbe, 'ok') || !!stringField(pythonProbe, 'version');
  const uvOk = boolField(uvProbe, 'ok') || !!stringField(uvProbe, 'version');
  const overallOk = pythonOk && uvOk;

  const handleEnvAction = async (env: AnyRecord, action: 'sync' | 'repair') => {
    const key = environmentKey(env);
    setPendingActionKey(key);
    try {
      await runEnvironmentAction(env, action);
    } finally {
      setPendingActionKey(null);
    }
  };

  return (
    <div className="section">
      <div className="hero sticky-hero">
        <div>
          <h2>Python 工具链</h2>
          <div className="meta">CPython + uv 托管环境</div>
        </div>
        <div className="actions">
          <button className="btn ghost" type="button" onClick={reloadPythonToolchainSurfaces}>重新加载</button>
          <button className="btn ghost" type="button" onClick={probeToolchainDraft}>探测 (Probe)</button>
          <button className="btn primary" type="button" onClick={saveToolchainDraft}>保存配置</button>
        </div>
      </div>

      {/* Overall health */}
      <div className="grid compact" style={{ marginBottom: 8 }}>
        <div className="field">
          <label>工具链状态</label>
          <span className={classNames('badge', overallOk ? 'ok' : 'warn')}>
            {overallOk ? '就绪' : '需检查'}
          </span>
          <span className="meta">Python / uv 探测结果</span>
        </div>
        <div className="field">
          <label>已管理环境</label>
          <span className="badge">{environments.length}</span>
          <span className="meta">点击下方卡片可 Sync / Repair</span>
        </div>
      </div>

      {/* Probe summary - structured */}
      <div className="slot" style={{ marginBottom: 16 }}>
        <div className="slot-head"><strong>探测结果</strong></div>
        <div className="grid compact">
          <ProbeCard label="Python" data={pythonProbe} />
          <ProbeCard label="uv" data={uvProbe} />
          <ProbeCard label="绑定" data={bindingProbe} />
        </div>
      </div>

      {/* Config - grouped */}
      <div className="slot" style={{ marginBottom: 16 }}>
        <div className="slot-head"><strong>基础设置</strong></div>
        <div className="grid compact">
          <label className="check"><input id="python-toolchain-enabled-input" type="checkbox" checked={boolField(toolchainDraft, 'enabled')} onChange={event => updateToolchainPath(['enabled'], event.target.checked)} /> 启用 Python 工具链</label>
          <label className="check"><input id="python-toolchain-system-certs-input" type="checkbox" checked={boolField(toolchainDraft, 'use_system_certificates')} onChange={event => updateToolchainPath(['use_system_certificates'], event.target.checked)} /> 使用系统证书</label>
        </div>
      </div>

      <div className="slot" style={{ marginBottom: 16 }}>
        <div className="slot-head"><strong>可执行文件路径</strong></div>
        <div className="grid compact">
          <Field id="python-toolchain-python-input" label="python.exe 路径" value={stringField(toolchainDraft, 'python_executable')} onChange={value => updateToolchainPath(['python_executable'], value)} mono />
          <Field id="python-toolchain-uv-input" label="uv.exe 路径" value={stringField(toolchainDraft, 'uv_executable')} onChange={value => updateToolchainPath(['uv_executable'], value)} mono />
        </div>
      </div>

      <div className="slot" style={{ marginBottom: 16 }}>
        <div className="slot-head"><strong>版本约束</strong></div>
        <div className="grid compact">
          <Field id="python-toolchain-required-python-input" label="要求 Python 版本" value={stringField(toolchainDraft, 'required_python')} onChange={value => updateToolchainPath(['required_python'], value)} />
          <Field id="python-toolchain-min-uv-input" label="最低 uv 版本" value={stringField(toolchainDraft, 'minimum_uv_version')} onChange={value => updateToolchainPath(['minimum_uv_version'], value)} />
        </div>
      </div>

      <div className="slot" style={{ marginBottom: 16 }}>
        <div className="slot-head"><strong>存储与缓存</strong></div>
        <div className="grid compact">
          <Field id="python-toolchain-env-root-input" label="环境根目录" value={stringField(toolchainDraft, 'environment_root')} onChange={value => updateToolchainPath(['environment_root'], value)} mono />
          <Field id="python-toolchain-cache-input" label="uv 缓存目录" value={stringField(toolchainDraft, 'cache_dir')} onChange={value => updateToolchainPath(['cache_dir'], value)} mono />
          <Field id="python-toolchain-index-input" label="默认 PyPI Index" value={stringField(toolchainDraft, 'default_index')} onChange={value => updateToolchainPath(['default_index'], value)} mono />
          <Field id="python-toolchain-timeout-input" type="number" label="同步超时秒数" value={String(field(toolchainDraft, 'sync_timeout_seconds', ''))} onChange={value => updateToolchainPath(['sync_timeout_seconds'], toInt(value))} />
        </div>
      </div>

      {/* Environments */}
      <div className="row-head">
        <strong>托管环境（Environments）</strong>
        <span className="badge">{environments.length}</span>
      </div>

      {environments.length ? (
        <div className="grid compact">
          {environments.map(environment => (
            <EnvironmentCard
              key={environmentKey(environment)}
              environment={environment}
              isPending={pendingActionKey === environmentKey(environment)}
              onAction={handleEnvAction}
            />
          ))}
        </div>
      ) : (
        <div className="hint">暂无托管环境。保存配置并触发相关 Agent 后会自动创建。</div>
      )}

      {/* Raw debug data - collapsible */}
      <div style={{ marginTop: 16 }}>
        <button
          type="button"
          className="btn ghost mini"
          onClick={() => setShowRaw(!showRaw)}
        >
          {showRaw ? '隐藏原始数据' : '显示原始探测数据（调试）'}
        </button>
        {showRaw && (
          <pre className="code" id="python-toolchain-probe-json" style={{ marginTop: 8 }}>{pretty({ probe: probeResult, environments })}</pre>
        )}
      </div>
    </div>
  );
});

function ProbeCard({ label, data }: { label: string; data: AnyRecord }) {
  const ok = boolField(data, 'ok') || stringField(data, 'version') !== '' || stringField(data, 'path') !== '';
  const version = stringField(data, 'version');
  const path = stringField(data, 'path');
  const reason = stringField(data, 'reason');
  return (
    <div className="field" style={{ border: '1px solid var(--hair)', borderRadius: 10, padding: '8px 10px', background: 'rgba(255,255,255,0.65)' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <label style={{ margin: 0 }}>{label}</label>
        <span className={classNames('badge', ok ? 'ok' : 'warn')}>{ok ? 'OK' : '检查'}</span>
      </div>
      {version && <div className="mono" style={{ fontSize: '13px', fontWeight: 600 }}>{version}</div>}
      {path && <div className="mono" style={{ fontSize: '11px', color: 'var(--color-muted)', wordBreak: 'break-all' }}>{path}</div>}
      {reason && <div className="meta" style={{ color: 'var(--color-danger-ink)' }}>{reason}</div>}
      {!version && !path && <div className="meta">—</div>}
    </div>
  );
}

function EnvironmentCard({ environment, isPending, onAction }: {
  environment: AnyRecord;
  isPending: boolean;
  onAction: (env: AnyRecord, action: 'sync' | 'repair') => Promise<void>;
}) {
  const owner = field<AnyRecord>(environment, 'owner', {});
  const status = field<AnyRecord>(environment, 'status', {});
  const state = stringField(status, 'state') || 'unknown';
  const enabled = boolField(environment, 'enabled');
  const reason = stringField(status, 'reason');
  const runtimeKind = stringField(environment, 'runtime_kind') || stringField(owner, 'kind');
  const envDir = stringField(owner, 'env_dir');
  const syncedAt = stringField(status, 'synced_at') || stringField(field<AnyRecord>(status, 'marker', {}), 'synced_at');

  const isReady = state === 'ready';

  return (
    <div style={{
      border: '1px solid var(--hair)',
      borderRadius: 14,
      padding: '12px 14px',
      background: isReady ? 'rgba(224, 242, 236, 0.5)' : 'rgba(255,255,255,0.82)',
    }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
        <strong>{environmentTitle(owner)}</strong>
        <span className={classNames('badge', isReady ? 'ok' : 'warn')}>{state}</span>
        <span className="badge">{enabled ? '启用' : '禁用'}</span>
      </div>

      <div className="mono" style={{ fontSize: '12px', marginTop: 4, wordBreak: 'break-all', color: 'var(--color-muted)' }}>
        {envDir || '(env dir)'}
      </div>

      <div className="meta" style={{ marginTop: 4 }}>
        {runtimeKind} · {enabled ? 'enabled' : 'disabled'}
        {syncedAt ? ` · synced ${formatTime(syncedAt)}` : ''}
      </div>

      {reason && <div className="meta" style={{ color: 'var(--color-danger-ink)', marginTop: 4 }}>{reason}</div>}

      <div className="actions foot" style={{ marginTop: 10 }}>
        <button
          className="btn ghost mini"
          type="button"
          disabled={isPending}
          onClick={() => onAction(environment, 'sync')}
        >
          {isPending ? '处理中...' : 'Sync'}
        </button>
        <button
          className="btn danger mini"
          type="button"
          disabled={isPending}
          onClick={() => onAction(environment, 'repair')}
        >
          {isPending ? '处理中...' : 'Repair'}
        </button>
      </div>
    </div>
  );
}

function environmentTitle(owner: AnyRecord): string {
  const version = stringField(owner, 'version');
  const kind = stringField(owner, 'kind') || 'environment';
  const id = stringField(owner, 'id') || '-';
  return `${kind} · ${id}${version ? ` @ ${version}` : ''}`;
}

function environmentKey(environment: AnyRecord): string {
  const owner = field<AnyRecord>(environment, 'owner', {});
  return `${stringField(owner, 'kind')}:${stringField(owner, 'id')}:${stringField(owner, 'version')}`;
}
