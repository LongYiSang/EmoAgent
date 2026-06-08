import { memo } from 'react';
import { field, pretty } from '../../shared/lib/data';
import type { AnyRecord } from '../../shared/lib/api';
import { JsonSavePanel } from '../components/JsonSavePanel';

export type RetentionTabProps = {
  effectiveConfig: AnyRecord;
  retentionDraft: string;
  setRetentionDraft: (value: string) => void;
  saveRetention: () => Promise<void>;
};

export default memo(function RetentionTab({ effectiveConfig, retentionDraft, setRetentionDraft, saveRetention }: RetentionTabProps) {
  return (
    <JsonSavePanel
      title="Retention"
      id="retention"
      value={retentionDraft}
      onValue={setRetentionDraft}
      output={pretty(field(field(effectiveConfig, 'memory_core', {}), 'retention', {}))}
      onSave={saveRetention}
    />
  );
});
