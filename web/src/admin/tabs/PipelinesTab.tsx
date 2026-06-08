import { memo } from 'react';
import { field, pretty } from '../../shared/lib/data';
import type { AnyRecord } from '../../shared/lib/api';
import type { Provider } from '../protocol/adminApi';
import { llmPipelineKeys, memoryPipelineBindings, pipelineProviderOptions, pipelineThinkingOptions } from '../lib/adminData';
import { Field } from '../components/Field';

export type PipelinesTabProps = {
  providers: Provider[];
  memoryDraft: AnyRecord;
  updateMemoryPath: (path: string[], value: unknown) => void;
  savePipelines: () => Promise<void>;
};

export default memo(function PipelinesTab({ providers, memoryDraft, updateMemoryPath, savePipelines }: PipelinesTabProps) {
  const bindings = field<AnyRecord>(memoryDraft, 'provider_bindings', {});

  function setBinding(key: string, path: string[], value: unknown) {
    updateMemoryPath(['provider_bindings', key, ...path], value);
  }

  return (
    <div className="section">
      <div className="hero"><div><h2>管线</h2><div className="meta">通过 provider_id 与 model 选择 Provider/Model 绑定</div></div><button className="btn primary" id="save-pipelines" type="button" onClick={savePipelines}>保存</button></div>
      <div className="grid" id="pipeline-binding-form">
        {memoryPipelineBindings.map(([key, label]) => {
          const binding = field<AnyRecord>(bindings, key, {});
          return (
            <div className="pipeline-row" key={key}>
              <div className="field"><label htmlFor={`mem-${key}-provider`}>{label} provider_id</label><select id={`mem-${key}-provider`} value={String(binding.provider_id || '')} onChange={event => setBinding(key, ['provider_id'], event.target.value)}>{pipelineProviderOptions(providers, key, String(binding.provider_id || '')).map(option => <option key={option.value} value={option.value}>{option.label}</option>)}</select></div>
              <Field id={`mem-${key}-model`} label={`${label} model`} value={String(binding.model || '')} onChange={value => setBinding(key, ['model'], value)} mono />
              {llmPipelineKeys.has(key) && <div className="field"><label htmlFor={`mem-${key}-thinking`}>{label} Thinking 策略</label><select id={`mem-${key}-thinking`} value={String(field(field(binding, 'thinking', {}), 'type', ''))} onChange={event => setBinding(key, ['thinking', 'type'], event.target.value)}>{pipelineThinkingOptions(String(field(field(binding, 'thinking', {}), 'type', ''))).map(option => <option key={option.value} value={option.value}>{option.label}</option>)}</select></div>}
            </div>
          );
        })}
      </div>
      <pre className="code" id="pipelines-json">{pretty(bindings)}</pre>
    </div>
  );
});
