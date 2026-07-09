import type { AnyRecord } from '../../shared/lib/api';
import { field } from '../../shared/lib/data';
import type { AgentConfig, Persona, Provider, ProviderPreset } from '../protocol/adminApi';

export { matchesQuery } from '../../shared/lib/search';

export type TabID = 'overview' | 'providers' | 'agents' | 'personas' | 'chat-settings' | 'usage' | 'memory-core' | 'agent-affect' | 'prompt-center' | 'websearch-pipeline' | 'platforms' | 'pipelines' | 'retrieval-mirror' | 'python-toolchain' | 'sidecar' | 'privacy-forget' | 'retention' | 'diagnostics';

export type TabGroupID = 'overview' | 'agents' | 'capabilities' | 'runtime' | 'system';

export type AdminTabItem = {
  id: TabID;
  label: string;
  description: string;
};

export type AdminTabGroup = {
  id: TabGroupID;
  label: string;
  items: AdminTabItem[];
};

/** Grouped admin navigation: 总览 / 智能体 / 能力 / 运行 / 系统 */
export const tabGroups: AdminTabGroup[] = [
  {
    id: 'overview',
    label: '总览',
    items: [
      { id: 'overview', label: '系统总览', description: '一眼看到当前 Agent、模型、记忆、插件与近期用量状态。' },
    ],
  },
  {
    id: 'agents',
    label: '智能体',
    items: [
      { id: 'providers', label: '模型服务', description: '配置 LLM Provider、协议、密钥环境变量与可用模型。' },
      { id: 'agents', label: 'Agent 配置', description: '绑定 Emotion/Work 模型槽位、Persona 与上下文覆盖。' },
      { id: 'personas', label: 'Persona', description: '人设文案、口吻与 Work 进度话术模板。' },
      { id: 'chat-settings', label: '聊天设置', description: '流式输出、路由策略与分段回复节奏。' },
      { id: 'prompt-center', label: '提示词中心', description: '集中查看与编辑系统提示词片段。' },
      { id: 'agent-affect', label: 'Agent Affect', description: '情感状态、profile 与提交式更新流程。' },
    ],
  },
  {
    id: 'capabilities',
    label: '能力',
    items: [
      { id: 'memory-core', label: 'Memory Core', description: '记忆核心开关、功能面与 Natural Memory 任务。' },
      { id: 'retrieval-mirror', label: '检索', description: '检索参数、镜像与召回相关配置。' },
      { id: 'pipelines', label: 'Pipeline', description: '记忆管线各阶段的 Provider / Model 绑定。' },
      { id: 'websearch-pipeline', label: '搜索流水线', description: '联网搜索提供方、抽取与回传策略。' },
      { id: 'platforms', label: '消息平台', description: '外部消息平台接入与 Agent 映射。' },
    ],
  },
  {
    id: 'runtime',
    label: '运行',
    items: [
      { id: 'usage', label: 'Usage', description: 'Token 与调用用量统计，观察系统负载。' },
      { id: 'diagnostics', label: '诊断', description: '生效配置校验、配置问题与插件健康状态。' },
      { id: 'sidecar', label: 'Sidecar', description: 'Sidecar 进程与配套服务配置。' },
    ],
  },
  {
    id: 'system',
    label: '系统',
    items: [
      { id: 'python-toolchain', label: 'Python', description: 'Python 工具链、环境与依赖相关设置。' },
      { id: 'privacy-forget', label: '隐私', description: '隐私与遗忘策略的高级 JSON 配置。' },
      { id: 'retention', label: '保留策略', description: '记忆与日志保留周期等策略配置。' },
    ],
  },
];

export const tabs: AdminTabItem[] = tabGroups.flatMap(group => group.items);

export function findAdminTab(id: TabID): AdminTabItem | undefined {
  return tabs.find(item => item.id === id);
}

export function findAdminGroupForTab(id: TabID): AdminTabGroup | undefined {
  return tabGroups.find(group => group.items.some(item => item.id === id));
}

export function isAdminTabID(value: string): value is TabID {
  return tabs.some(item => item.id === value);
}

/** Resolve admin tab from location hash, e.g. `#memory-core` → `memory-core`. */
export function tabIDFromHash(hash = typeof window !== 'undefined' ? window.location.hash : ''): TabID {
  const raw = hash.replace(/^#/, '').trim();
  return isAdminTabID(raw) ? raw : 'overview';
}

export const slotDefs = [
  ['emotion-main', '聊天主模型'],
  ['emotion-summary', '聊天摘要模型'],
  ['work-main', '干活主模型'],
  ['work-summary', '干活摘要模型'],
] as const;

export const memoryPipelineBindings = [
  ['prefilter', '预筛选'],
  ['extraction', '提取'],
  ['extraction_repair', '提取修复'],
  ['embedding', 'Embedding'],
  ['query_analysis', '查询分析'],
  ['rerank', 'Rerank'],
  ['curation', '整理'],
] as const;

export const llmPipelineKeys = new Set(['prefilter', 'extraction', 'extraction_repair', 'query_analysis', 'curation']);

export const slotParams = [
  'max_tokens',
  'temperature',
  'stream',
  'top_p',
  'presence_penalty',
  'frequency_penalty',
  'reasoning_effort',
  'thinking_mode',
  'thinking_budget',
  'thinking_effort',
  'extra',
] as const;

export type SlotParam = typeof slotParams[number];

export function emptyProvider(): Provider {
  return { protocol: 'openai_compatible', model_discovery: 'manual', capabilities: ['chat'], enabled: true };
}

export function emptyAgent(): AgentConfig {
  return { emotion: { main: {}, summary: {} }, work: { main: {}, summary: {} }, context_overrides: {}, user_address: { preferred: [], usage: 'natural' } };
}

export function emptyPersona(): Persona {
  return { work_progress_phrases: {}, quirks: [] };
}

export function cloneRecord<T extends AnyRecord>(value: T): T {
  return JSON.parse(JSON.stringify(value || {})) as T;
}

export function parseJSONRecord(value: string, fallback?: unknown): AnyRecord {
  try {
    const parsed = JSON.parse(value || '{}');
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) return parsed as AnyRecord;
    throw new Error('JSON 必须是对象');
  } catch (error) {
    if (fallback && typeof fallback === 'object' && !Array.isArray(fallback)) return fallback as AnyRecord;
    throw error;
  }
}

export function setNestedValue<T extends AnyRecord>(root: T, path: string[], value: unknown): T {
  const next = cloneRecord(root);
  let cursor: AnyRecord = next;
  for (const key of path.slice(0, -1)) {
    const existing = cursor[key];
    if (!existing || typeof existing !== 'object' || Array.isArray(existing)) cursor[key] = {};
    cursor = cursor[key] as AnyRecord;
  }
  const last = path[path.length - 1];
  cursor[last] = value;
  return cleanDeep(next);
}

export function setNested<T extends AnyRecord>(root: T, setter: (value: T) => void, path: string[], value: unknown) {
  setter(setNestedValue(root, path, value));
}

export function cleanDeep<T>(value: T): T {
  if (Array.isArray(value)) return value.map(cleanDeep) as T;
  if (value && typeof value === 'object') {
    const record = value as AnyRecord;
    for (const key of Object.keys(record)) {
      const child = cleanDeep(record[key]);
      if (child === undefined || child === '' || child === null || (typeof child === 'object' && !Array.isArray(child) && Object.keys(child as AnyRecord).length === 0)) {
        delete record[key];
      } else {
        record[key] = child;
      }
    }
  }
  return value;
}

export function pipelineProviderOptions(providers: Provider[], key: string, selected: string): Array<{ value: string; label: string }> {
  const accepts = (provider: Provider) => {
    const caps = Array.isArray(provider.capabilities) && provider.capabilities.length ? provider.capabilities : ['chat'];
    if (key === 'embedding') return caps.includes('embedding') || provider.id === selected;
    if (key === 'rerank') return caps.includes('rerank') || provider.id === selected;
    return caps.includes('chat') || provider.id === selected;
  };
  return [{ value: '', label: '选择 Provider' }, ...providers.filter(accepts).map(provider => ({ value: String(provider.id || ''), label: String(provider.name || provider.id || '') }))];
}

export function pipelineThinkingOptions(selected: string): Array<{ value: string; label: string }> {
  const values = ['', 'enabled', 'disabled'];
  const labels: Record<string, string> = {
    '': '继承',
    enabled: 'enabled',
    disabled: 'disabled',
  };
  return values.map(value => ({ value, label: labels[value] || value || (selected ? '继承' : '继承') }));
}

export function providerPresetForBinding(providers: Provider[], presets: ProviderPreset[], providerID: string) {
  const provider = providers.find(item => item.id === providerID);
  const presetID = String(provider?.preset_id || '');
  if (!presetID) return null;
  return presets.find(item => item.id === presetID) || null;
}

export function slotDefaults(slot: string, preset: ProviderPreset | null): AnyRecord {
  const admin = field<AnyRecord>(preset, 'admin', {});
  if (!Object.keys(admin).length) return {};
  return slot.endsWith('summary') ? field<AnyRecord>(admin, 'summary_defaults', {}) : field<AnyRecord>(admin, 'main_defaults', {});
}

export function recommendedParamValue(defaults: AnyRecord, param: SlotParam): unknown {
  const thinking = field<AnyRecord>(defaults, 'thinking', {});
  switch (param) {
    case 'max_tokens': return defaults.max_tokens;
    case 'temperature': return defaults.temperature;
    case 'stream': return defaults.stream;
    case 'top_p': return defaults.top_p;
    case 'presence_penalty': return defaults.presence_penalty;
    case 'frequency_penalty': return defaults.frequency_penalty;
    case 'reasoning_effort': return defaults.reasoning_effort;
    case 'thinking_mode': return thinking.mode;
    case 'thinking_budget': return thinking.budget_tokens;
    case 'thinking_effort': return thinking.effort;
    case 'extra': return defaults.extra;
    default: return undefined;
  }
}

export function currentSlotParamValue(params: AnyRecord, param: SlotParam): unknown {
  const thinking = field<AnyRecord>(params, 'thinking', {});
  switch (param) {
    case 'thinking_mode': return thinking.mode;
    case 'thinking_budget': return thinking.budget_tokens;
    case 'thinking_effort': return thinking.effort;
    default: return params[param];
  }
}

export function hasSlotParamValue(params: AnyRecord, param: SlotParam) {
  const value = currentSlotParamValue(params, param);
  if (value === undefined || value === null || value === '') return false;
  if (typeof value === 'object' && !Array.isArray(value)) return Object.keys(value as AnyRecord).length > 0;
  return true;
}

export function writeSlotParam(params: AnyRecord, param: SlotParam, value: unknown) {
  if (param === 'thinking_mode' || param === 'thinking_budget' || param === 'thinking_effort') {
    if (!params.thinking || typeof params.thinking !== 'object' || Array.isArray(params.thinking)) params.thinking = {};
    const thinking = params.thinking as AnyRecord;
    if (param === 'thinking_mode') thinking.mode = value;
    if (param === 'thinking_budget') thinking.budget_tokens = value;
    if (param === 'thinking_effort') thinking.effort = value;
    return;
  }
  params[param] = value;
}

export function slotParamMeta(slot: string, params: AnyRecord, preset: ProviderPreset | null, param: SlotParam) {
  const admin = field<AnyRecord>(preset, 'admin', {});
  const visible = Array.isArray(admin.visible_params) && admin.visible_params.length ? new Set(admin.visible_params.map(String)) : null;
  const supported = !visible || visible.has(param);
  const hasValue = hasSlotParamValue(params, param);
  const recommended = recommendedParamValue(slotDefaults(slot, preset), param);
  if (!supported && hasValue) return { hidden: false, warn: true, note: '当前 Provider 可能会忽略该值。' };
  if (!supported) return { hidden: true, warn: false, note: '' };
  if (recommended !== undefined) return { hidden: false, warn: false, note: `推荐值：${formatRecommendedValue(recommended)}` };
  return { hidden: false, warn: false, note: '' };
}

export function formatRecommendedValue(value: unknown) {
  if (value === undefined) return '';
  if (value && typeof value === 'object') return JSON.stringify(value);
  return String(value);
}
