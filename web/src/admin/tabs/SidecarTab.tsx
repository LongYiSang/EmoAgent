import { memo } from 'react';
import { boolField, field, pretty, toInt } from '../../shared/lib/data';
import type { AnyRecord } from '../../shared/lib/api';
import type { SidecarAdmin } from '../hooks/useSidecarAdmin';
import { Field } from '../components/Field';

export type SidecarTabProps = SidecarAdmin & {
  memoryDraft: AnyRecord;
  updateMemoryPath: (path: string[], value: unknown) => void;
  saveSidecarConfig: () => Promise<void>;
};

export default memo(function SidecarTab({
  memoryDraft,
  updateMemoryPath,
  saveSidecarConfig,
  sidecarStatus,
  sidecarGenerated,
  sidecarLogs,
  reloadSidecar,
  runSidecarAction,
}: SidecarTabProps) {
  const sidecar = field<AnyRecord>(memoryDraft, 'sidecar', {});

  function setSidecar(key: string, value: unknown) {
    updateMemoryPath(['sidecar', key], value);
  }

  return (
    <div className="section">
      <div className="hero"><div><h2>生成的 TOML</h2><div className="meta">仅用于预览；API Key 仍保存在环境变量中</div></div><div className="actions"><button className="btn primary" id="sidecar-start" type="button" onClick={() => runSidecarAction('start')}>启动</button><button className="btn ghost" id="sidecar-stop" type="button" onClick={() => runSidecarAction('stop')}>停止</button><button className="btn ghost" id="sidecar-restart" type="button" onClick={() => runSidecarAction('restart')}>重启</button><button className="btn primary" id="save-sidecar-config" type="button" onClick={saveSidecarConfig}>保存</button><button className="btn ghost" id="sidecar-reload" type="button" onClick={reloadSidecar}>重新加载</button></div></div>
      <div className="grid">
        <label className="check"><input id="sidecar-enabled-input" type="checkbox" checked={boolField(sidecar, 'enabled')} onChange={event => setSidecar('enabled', event.target.checked)} /> 启用</label>
        <label className="check"><input id="sidecar-managed-input" type="checkbox" checked={boolField(sidecar, 'managed')} onChange={event => setSidecar('managed', event.target.checked)} /> 托管</label>
        <Field id="sidecar-adapter-input" label="Adapter" value={String(sidecar.adapter || '')} onChange={value => setSidecar('adapter', value)} />
        <Field id="sidecar-host-input" label="Host" value={String(sidecar.host || '')} onChange={value => setSidecar('host', value)} />
        <Field id="sidecar-port-input" type="number" label="Port" value={String(sidecar.port || '')} onChange={value => setSidecar('port', toInt(value))} />
        <Field id="sidecar-url-input" label="URL" value={String(sidecar.url || '')} onChange={value => setSidecar('url', value)} mono />
        <Field id="sidecar-working-dir-input" label="工作目录" value={String(sidecar.working_dir || '')} onChange={value => setSidecar('working_dir', value)} mono />
        <Field id="sidecar-config-path-input" label="生成配置路径" value={String(sidecar.config_path || '')} onChange={value => setSidecar('config_path', value)} mono />
      </div>
      <pre className="code" id="sidecar-runtime-json">{pretty({ status: sidecarStatus })}</pre>
      <pre className="code" id="sidecar-generated-config">{sidecarGenerated}</pre>
      <pre className="code" id="sidecar-logs">{sidecarLogs}</pre>
    </div>
  );
});
