import { memo } from 'react';
import { field, pretty } from '../../shared/lib/data';
import type { AnyRecord } from '../../shared/lib/api';
import { JsonSavePanel } from '../components/JsonSavePanel';

export type PrivacyForgetTabProps = {
  memoryDraft: AnyRecord;
  effectiveConfig: AnyRecord;
  privacyDraft: string;
  setPrivacyDraft: (value: string) => void;
  savePrivacyForget: () => Promise<void>;
};

export default memo(function PrivacyForgetTab({ memoryDraft, effectiveConfig, privacyDraft, setPrivacyDraft, savePrivacyForget }: PrivacyForgetTabProps) {
  return (
    <JsonSavePanel
      title="隐私/遗忘"
      id="privacy-forget"
      value={privacyDraft}
      onValue={setPrivacyDraft}
      description="隐私与遗忘策略的高级 JSON 配置。修改前请确认影响范围。"
      output={pretty({ memory: field(memoryDraft, 'forgetting_privacy', {}), effective: field(field(effectiveConfig, 'memory_core', {}), 'forgetting_privacy', {}) })}
      onSave={savePrivacyForget}
    />
  );
});
