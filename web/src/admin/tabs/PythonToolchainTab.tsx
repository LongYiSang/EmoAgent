import { memo } from 'react';
import type { AnyRecord } from '../../shared/lib/api';
import { boolField, field, pretty, stringField, toInt } from '../../shared/lib/data';
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
  return (
    <div className="section">
      <div className="hero">
        <div>
          <h2>Python Toolchain</h2>
          <div className="meta">CPython 3.12 与 uv</div>
        </div>
        <div className="actions">
          <button className="btn ghost" type="button" onClick={reloadPythonToolchainSurfaces}>重新加载</button>
          <button className="btn ghost" type="button" onClick={probeToolchainDraft}>Probe</button>
          <button className="btn primary" type="button" onClick={saveToolchainDraft}>保存</button>
        </div>
      </div>

      <div className="grid">
        <label className="check"><input id="python-toolchain-enabled-input" type="checkbox" checked={boolField(toolchainDraft, 'enabled')} onChange={event => updateToolchainPath(['enabled'], event.target.checked)} /> 启用</label>
        <label className="check"><input id="python-toolchain-system-certs-input" type="checkbox" checked={boolField(toolchainDraft, 'use_system_certificates')} onChange={event => updateToolchainPath(['use_system_certificates'], event.target.checked)} /> 系统证书</label>
        <Field id="python-toolchain-python-input" label="python.exe" value={stringField(toolchainDraft, 'python_executable')} onChange={value => updateToolchainPath(['python_executable'], value)} mono />
        <Field id="python-toolchain-uv-input" label="uv.exe" value={stringField(toolchainDraft, 'uv_executable')} onChange={value => updateToolchainPath(['uv_executable'], value)} mono />
        <Field id="python-toolchain-required-python-input" label="Python" value={stringField(toolchainDraft, 'required_python')} onChange={value => updateToolchainPath(['required_python'], value)} />
        <Field id="python-toolchain-min-uv-input" label="Min uv" value={stringField(toolchainDraft, 'minimum_uv_version')} onChange={value => updateToolchainPath(['minimum_uv_version'], value)} />
        <Field id="python-toolchain-env-root-input" label="Env Root" value={stringField(toolchainDraft, 'environment_root')} onChange={value => updateToolchainPath(['environment_root'], value)} mono />
        <Field id="python-toolchain-cache-input" label="uv Cache" value={stringField(toolchainDraft, 'cache_dir')} onChange={value => updateToolchainPath(['cache_dir'], value)} mono />
        <Field id="python-toolchain-index-input" label="Index" value={stringField(toolchainDraft, 'default_index')} onChange={value => updateToolchainPath(['default_index'], value)} mono />
        <Field id="python-toolchain-timeout-input" type="number" label="Sync Timeout" value={String(field(toolchainDraft, 'sync_timeout_seconds', ''))} onChange={value => updateToolchainPath(['sync_timeout_seconds'], toInt(value))} />
      </div>

      <div className="grid compact">
        <ProbeBadge label="Python" value={field(probeResult, 'python', {})} />
        <ProbeBadge label="uv" value={field(probeResult, 'uv', {})} />
        <ProbeBadge label="Binding" value={field(probeResult, 'binding', {})} />
      </div>

      <div className="row-head">
        <strong>Environments</strong>
        <span className="badge">{environments.length}</span>
      </div>
      <div className="grid compact">
        {environments.length ? environments.map(environment => (
          <EnvironmentPanel
            key={environmentKey(environment)}
            environment={environment}
            runEnvironmentAction={runEnvironmentAction}
          />
        )) : <div className="hint">暂无环境</div>}
      </div>

      <pre className="code" id="python-toolchain-probe-json">{pretty({ probe: probeResult, environments })}</pre>
    </div>
  );
});

function ProbeBadge({ label, value }: { label: string; value: AnyRecord }) {
  const ok = boolField(value, 'ok') || stringField(value, 'version') !== '' || stringField(value, 'path') !== '';
  const text = stringField(value, 'version') || stringField(value, 'path') || (Object.keys(value).length ? 'ok' : '-');
  return (
    <div className="field">
      <label>{label}</label>
      <span className={classNames('badge', ok ? 'ok' : 'warn')}>{ok ? 'OK' : '-'}</span>
      <span className="mono">{text}</span>
    </div>
  );
}

function EnvironmentPanel({ environment, runEnvironmentAction }: { environment: AnyRecord; runEnvironmentAction: (environment: AnyRecord, action: 'sync' | 'repair') => Promise<void> }) {
  const owner = field<AnyRecord>(environment, 'owner', {});
  const status = field<AnyRecord>(environment, 'status', {});
  const state = stringField(status, 'state') || 'unknown';
  const enabled = boolField(environment, 'enabled');
  return (
    <div className="field">
      <label>{environmentTitle(owner)}</label>
      <span className={classNames('badge', state === 'ready' ? 'ok' : 'warn')}>{state}</span>
      <span className="mono">{stringField(owner, 'env_dir')}</span>
      <span className="meta">{enabled ? 'enabled' : 'disabled'} · {stringField(environment, 'runtime_kind') || stringField(owner, 'kind')}</span>
      <div className="actions foot">
        <button className="btn ghost" type="button" onClick={() => runEnvironmentAction(environment, 'sync')}>Sync</button>
        <button className="btn ghost" type="button" onClick={() => runEnvironmentAction(environment, 'repair')}>Repair</button>
      </div>
      {stringField(status, 'reason') ? <span className="mono">{stringField(status, 'reason')}</span> : null}
    </div>
  );
}

function environmentTitle(owner: AnyRecord): string {
  const version = stringField(owner, 'version');
  return `${stringField(owner, 'kind') || 'environment'} · ${stringField(owner, 'id') || '-'}${version ? ` @ ${version}` : ''}`;
}

function environmentKey(environment: AnyRecord): string {
  const owner = field<AnyRecord>(environment, 'owner', {});
  return `${stringField(owner, 'kind')}:${stringField(owner, 'id')}:${stringField(owner, 'version')}`;
}
