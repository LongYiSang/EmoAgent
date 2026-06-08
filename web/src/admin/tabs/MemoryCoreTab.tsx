import { memo } from 'react';
import { boolField, field, pretty, toInt } from '../../shared/lib/data';
import type { AnyRecord } from '../../shared/lib/api';
import { Field } from '../components/Field';

export type MemoryCoreTabProps = {
  effectiveConfig: AnyRecord;
  memoryDraft: AnyRecord;
  memoryFeatures: AnyRecord;
  memoryJobs: AnyRecord[];
  memorySegments: AnyRecord[];
  naturalMemoryLatest: AnyRecord;
  reloadMemorySurfaces: () => Promise<void>;
  reloadNaturalLatest: () => Promise<void>;
  runNaturalMemoryNow: (dryRun: boolean) => Promise<void>;
  saveMemoryCore: () => Promise<void>;
  saveMemoryFeaturesDraft: () => Promise<void>;
  patchMemoryDraft: (key: string, value: unknown) => void;
  updateMemoryPath: (path: string[], value: unknown) => void;
};

export default memo(function MemoryCoreTab({
  effectiveConfig,
  memoryDraft,
  memoryFeatures,
  memoryJobs,
  memorySegments,
  naturalMemoryLatest,
  reloadMemorySurfaces,
  reloadNaturalLatest,
  runNaturalMemoryNow,
  saveMemoryCore,
  saveMemoryFeaturesDraft,
  patchMemoryDraft,
  updateMemoryPath,
}: MemoryCoreTabProps) {
  const natural = field<AnyRecord>(memoryDraft, 'natural_memory', {});
  const manual = field<AnyRecord>(natural, 'manual', {});
  const latestRun = field<AnyRecord>(naturalMemoryLatest, 'natural_run', field(naturalMemoryLatest, 'NaturalRun', {}));

  function setNatural(path: string[], value: unknown) {
    updateMemoryPath(['natural_memory', ...path], value);
  }

  return (
    <div className="section">
      <div className="hero"><div><h2>Memory Core</h2><div className="meta">Seed + runtime + MemoryCore effective config</div></div><button className="btn primary" id="save-memory-core" type="button" onClick={saveMemoryCore}>Save</button></div>
      <div className="grid">
        <label className="check"><input id="memory-enabled" type="checkbox" checked={boolField(memoryDraft, 'enabled')} onChange={event => patchMemoryDraft('enabled', event.target.checked)} /> Enabled</label>
        <Field id="memory-config-path" label="Config path" value={String(memoryDraft.config_path || '')} onChange={value => patchMemoryDraft('config_path', value)} mono />
        <Field id="memory-manual-rules" label="Manual rules path" value={String(memoryDraft.manual_rules_path || '')} onChange={value => patchMemoryDraft('manual_rules_path', value)} mono />
        <div className="field"><label>Latest status</label><span className="badge" id="natural-memory-latest-status">{String(field(latestRun, 'status', 'none'))}</span></div>
      </div>
      <div className="section nested" id="memory-features-card">
        <div className="hero"><div><h3>Memory Runtime</h3><div className="meta">Features, extraction jobs and finalized segments</div></div><div className="actions"><button className="btn ghost" id="memory-surfaces-reload" type="button" onClick={reloadMemorySurfaces}>Reload</button><button className="btn primary" id="save-memory-features" type="button" onClick={saveMemoryFeaturesDraft}>Save Features</button></div></div>
        <div className="grid">
          <div className="field"><label>Features</label><span className="badge" id="memory-features-enabled">{String(field(memoryFeatures, 'enabled', field(memoryFeatures, 'status', 'unknown')))}</span></div>
          <div className="field"><label>Extraction jobs</label><span className="badge" id="memory-extraction-count">{memoryJobs.length}</span></div>
          <div className="field"><label>Segments</label><span className="badge" id="memory-segment-count">{memorySegments.length}</span></div>
        </div>
        <pre className="code" id="memory-features-json">{pretty({ features: memoryFeatures, jobs: memoryJobs.slice(0, 10), segments: memorySegments.slice(0, 10) })}</pre>
      </div>
      <div className="section nested" id="natural-memory-card">
        <div className="hero"><div><h3>Natural Memory</h3><div className="meta" id="natural-memory-latest-run">{[field(latestRun, 'run_kind', ''), field(latestRun, 'run_id', '')].filter(Boolean).join(' · ') || 'none'}</div></div><div className="actions"><button className="btn ghost" id="natural-memory-reload" type="button" onClick={reloadNaturalLatest}>Latest</button><button className="btn ghost" id="natural-memory-dry-run" type="button" onClick={() => runNaturalMemoryNow(true)}>Dry-run</button><button className="btn primary" id="natural-memory-run-now" type="button" onClick={() => runNaturalMemoryNow(false)}>Run now</button><button className="btn primary" id="save-natural-memory" type="button" onClick={saveMemoryCore}>Save</button></div></div>
        <div className="grid">
          <label className="check"><input id="natural-memory-enabled" type="checkbox" checked={boolField(natural, 'enabled')} onChange={event => setNatural(['enabled'], event.target.checked)} /> Enabled</label>
          <label className="check"><input id="natural-memory-scheduler" type="checkbox" checked={boolField(natural, 'scheduler_enabled')} onChange={event => setNatural(['scheduler_enabled'], event.target.checked)} /> Scheduler</label>
          <label className="check"><input id="natural-memory-run-missed" type="checkbox" checked={boolField(natural, 'run_missed_on_start')} onChange={event => setNatural(['run_missed_on_start'], event.target.checked)} /> Run missed</label>
          <Field id="natural-memory-tick-interval" type="number" label="Tick seconds" value={String(natural.tick_interval_seconds || '')} onChange={value => setNatural(['tick_interval_seconds'], toInt(value))} />
          <Field id="natural-memory-local-time" label="Local time" value={String(natural.local_time || '')} onChange={value => setNatural(['local_time'], value)} mono />
          <Field id="natural-memory-timezone" label="Timezone" value={String(natural.timezone || '')} onChange={value => setNatural(['timezone'], value)} mono />
          <label className="check"><input id="natural-memory-manual-enabled" type="checkbox" checked={boolField(manual, 'enabled')} onChange={event => setNatural(['manual', 'enabled'], event.target.checked)} /> Manual</label>
          <label className="check"><input id="natural-memory-manual-dry-run" type="checkbox" checked={boolField(manual, 'allow_dry_run')} onChange={event => setNatural(['manual', 'allow_dry_run'], event.target.checked)} /> Allow dry-run</label>
        </div>
        <pre className="code" id="natural-memory-json">{pretty({ host: natural, latest: naturalMemoryLatest })}</pre>
      </div>
      <pre className="code" id="memory-core-json">{pretty({ memory: memoryDraft, memory_core: field(effectiveConfig, 'memory_core', {}) })}</pre>
    </div>
  );
});
