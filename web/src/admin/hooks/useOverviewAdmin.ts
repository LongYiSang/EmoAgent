import { useCallback, useMemo, useState } from 'react';
import type { AnyRecord } from '../../shared/lib/api';
import { boolField, field, numberField, stringField } from '../../shared/lib/data';
import {
  loadAgents,
  loadConfigIssues,
  loadEffectiveConfig,
  loadPersonas,
  loadPlatformStatus,
  loadPluginDiagnostics,
  loadProviders,
  loadPythonToolchain,
  loadSidecarStatus,
  type AgentConfig,
  type Persona,
  type Provider,
} from '../protocol/adminApi';
import { loadPlugins, type PluginSummary } from '../protocol/pluginApi';
import { loadUsageEvents, loadUsageSummary, type LLMUsageEvent, type LLMUsageSummaryRow } from '../protocol/usageApi';
import type { AdminStatusControls } from './useAdminStatus';

export type OverviewHealth = {
  id: string;
  label: string;
  detail: string;
  tone: 'ok' | 'warn' | 'muted';
  tab?: string;
};

export type OverviewSnapshot = {
  providers: Provider[];
  agents: AgentConfig[];
  activeAgentID: string;
  personas: Persona[];
  effectiveConfig: AnyRecord;
  configIssues: AnyRecord[];
  plugins: PluginSummary[];
  pluginDiagnostics: AnyRecord;
  sidecarStatus: AnyRecord;
  platformStatus: AnyRecord;
  pythonToolchain: AnyRecord;
  usageEvents: LLMUsageEvent[];
  usageSummary: LLMUsageSummaryRow[];
};

const emptySnapshot: OverviewSnapshot = {
  providers: [],
  agents: [],
  activeAgentID: '',
  personas: [],
  effectiveConfig: {},
  configIssues: [],
  plugins: [],
  pluginDiagnostics: {},
  sidecarStatus: {},
  platformStatus: {},
  pythonToolchain: {},
  usageEvents: [],
  usageSummary: [],
};

type OverviewAdminOptions = Pick<AdminStatusControls, 'setStatus' | 'showError'>;

export function useOverviewAdmin({ setStatus, showError }: OverviewAdminOptions) {
  const [snapshot, setSnapshot] = useState<OverviewSnapshot>(emptySnapshot);
  const [loading, setLoading] = useState(false);
  const [loadedAt, setLoadedAt] = useState('');
  const [error, setError] = useState('');

  const reloadOverview = useCallback(async () => {
    setLoading(true);
    setError('');
    setStatus('正在加载系统总览...');
    try {
      const results = await Promise.allSettled([
        loadProviders(),
        loadAgents(),
        loadPersonas(),
        loadEffectiveConfig(),
        loadConfigIssues(),
        loadPlugins(),
        loadPluginDiagnostics(),
        loadSidecarStatus(),
        loadPlatformStatus(),
        loadPythonToolchain(),
        loadUsageEvents({ limit: '12' }),
        loadUsageSummary({ group_by: 'component', limit: '12' }),
      ]);

      const value = <T,>(index: number, fallback: T): T => {
        const item = results[index];
        return item.status === 'fulfilled' ? item.value as T : fallback;
      };

      const agentsPayload = value(1, { configs: [] as AgentConfig[], activeID: '' });
      const sidecarPayload = value(7, { status: {} as AnyRecord, generated: '', logs: '' });

      const next: OverviewSnapshot = {
        providers: value(0, [] as Provider[]),
        agents: agentsPayload.configs || [],
        activeAgentID: agentsPayload.activeID || '',
        personas: value(2, [] as Persona[]),
        effectiveConfig: value(3, {} as AnyRecord),
        configIssues: value(4, [] as AnyRecord[]),
        plugins: value(5, [] as PluginSummary[]),
        pluginDiagnostics: value(6, {} as AnyRecord),
        sidecarStatus: sidecarPayload.status || {},
        platformStatus: value(8, {} as AnyRecord),
        pythonToolchain: value(9, {} as AnyRecord),
        usageEvents: value(10, [] as LLMUsageEvent[]),
        usageSummary: value(11, [] as LLMUsageSummaryRow[]),
      };

      setSnapshot(next);
      setLoadedAt(new Date().toISOString());

      const failed = results.filter(item => item.status === 'rejected').length;
      if (failed > 0) {
        setError(`${failed} 项数据加载失败，已展示可用部分`);
        setStatus('总览部分就绪');
      } else {
        setStatus('就绪');
      }
    } catch (err) {
      showError(err);
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, [setStatus, showError]);

  const model = useMemo(() => buildOverviewModel(snapshot), [snapshot]);

  return useMemo(() => ({
    ...snapshot,
    ...model,
    loading,
    loadedAt,
    error,
    reloadOverview,
  }), [snapshot, model, loading, loadedAt, error, reloadOverview]);
}

export type OverviewAdmin = ReturnType<typeof useOverviewAdmin>;

function buildOverviewModel(snapshot: OverviewSnapshot) {
  const providerEnabledCount = snapshot.providers.filter(provider => provider.enabled !== false).length;
  const activeAgent = snapshot.agents.find(agent => agent.id === snapshot.activeAgentID);
  const activePersona = stringField(activeAgent, 'persona_key') || stringField(activeAgent, 'persona');

  const memory = field<AnyRecord>(snapshot.effectiveConfig, 'memory', field(snapshot.effectiveConfig, 'memory_core', {}));
  const memoryEnabled = boolField(memory, 'enabled') || boolField(field(snapshot.effectiveConfig, 'memory', {}), 'enabled');

  const issueCount = snapshot.configIssues.length;
  const errorIssues = snapshot.configIssues.filter(issue => String(issue.severity || '').toLowerCase() === 'error').length;

  const pluginsEnabled = snapshot.plugins.filter(plugin => boolField(plugin, 'enabled') || stringField(plugin, 'status') === 'enabled' || stringField(plugin, 'state') === 'enabled').length;
  // fallback: many plugins use `enabled` boolean
  const pluginEnabledCount = snapshot.plugins.filter(plugin => plugin.enabled === true || boolField(plugin, 'enabled')).length;

  let usageRequests = 0;
  let usageErrors = 0;
  let usageTokens = 0;
  for (const row of snapshot.usageSummary) {
    usageRequests += numberField(row, 'request_count');
    usageErrors += numberField(row, 'error_count');
    usageTokens += numberField(row, 'total_tokens');
  }

  const sidecarState = stringField(snapshot.sidecarStatus, 'status')
    || stringField(snapshot.sidecarStatus, 'state')
    || stringField(snapshot.sidecarStatus, 'running');
  const sidecarRunning = /run|ok|ready|true|up|active/i.test(sidecarState)
    || boolField(snapshot.sidecarStatus, 'running')
    || boolField(snapshot.sidecarStatus, 'alive');

  const platformConnected = boolField(snapshot.platformStatus, 'connected')
    || stringField(snapshot.platformStatus, 'status') === 'connected'
    || boolField(field(snapshot.platformStatus, 'transport', {}), 'connected');

  const pythonOk = boolField(snapshot.pythonToolchain, 'ready')
    || boolField(field(snapshot.pythonToolchain, 'probe', {}), 'ok')
    || stringField(snapshot.pythonToolchain, 'status') === 'ok'
    || stringField(field(snapshot.pythonToolchain, 'python', {}), 'available') === 'true'
    || boolField(field(snapshot.pythonToolchain, 'python', {}), 'available');

  const pluginDiagStatus = stringField(snapshot.pluginDiagnostics, 'status') || 'unknown';
  const pluginDiagOk = !/error|fail|degraded/i.test(pluginDiagStatus);

  const health: OverviewHealth[] = [
    {
      id: 'agent',
      label: '当前 Agent',
      detail: activeAgent
        ? `${stringField(activeAgent, 'name') || activeAgent.id}${activePersona ? ` · Persona ${activePersona}` : ''}`
        : (snapshot.activeAgentID ? snapshot.activeAgentID : '未设置当前 Agent'),
      tone: activeAgent ? 'ok' : 'warn',
      tab: 'agents',
    },
    {
      id: 'providers',
      label: '模型服务',
      detail: snapshot.providers.length
        ? `${providerEnabledCount}/${snapshot.providers.length} 已启用`
        : '尚未配置 Provider',
      tone: providerEnabledCount > 0 ? 'ok' : 'warn',
      tab: 'providers',
    },
    {
      id: 'memory',
      label: 'Memory',
      detail: memoryEnabled ? '记忆核心已启用' : '记忆核心未启用或未配置',
      tone: memoryEnabled ? 'ok' : 'muted',
      tab: 'memory-core',
    },
    {
      id: 'sidecar',
      label: 'Sidecar',
      detail: sidecarState ? `状态：${sidecarState}` : '无状态信息',
      tone: sidecarRunning ? 'ok' : (sidecarState ? 'warn' : 'muted'),
      tab: 'sidecar',
    },
    {
      id: 'plugins',
      label: '插件',
      detail: snapshot.plugins.length
        ? `${Math.max(pluginsEnabled, pluginEnabledCount)}/${snapshot.plugins.length} · 诊断 ${pluginDiagStatus}`
        : `暂无插件 · 诊断 ${pluginDiagStatus}`,
      tone: pluginDiagOk ? (snapshot.plugins.length ? 'ok' : 'muted') : 'warn',
      tab: 'diagnostics',
    },
    {
      id: 'platform',
      label: '消息平台',
      detail: platformConnected ? '已连接' : (stringField(snapshot.platformStatus, 'status') || '未连接 / 未启用'),
      tone: platformConnected ? 'ok' : 'muted',
      tab: 'platforms',
    },
    {
      id: 'python',
      label: 'Python 工具链',
      detail: pythonOk ? '可用' : (stringField(snapshot.pythonToolchain, 'status') || '待探测'),
      tone: pythonOk ? 'ok' : 'muted',
      tab: 'python-toolchain',
    },
    {
      id: 'config',
      label: '配置问题',
      detail: issueCount ? `${issueCount} 条（错误 ${errorIssues}）` : '暂无配置问题',
      tone: errorIssues > 0 ? 'warn' : (issueCount > 0 ? 'warn' : 'ok'),
      tab: 'diagnostics',
    },
  ];

  const stats = [
    {
      id: 'providers',
      label: 'Provider',
      value: String(snapshot.providers.length),
      hint: `${providerEnabledCount} 启用`,
      tone: providerEnabledCount > 0 ? 'sky' as const : 'warn' as const,
      tab: 'providers' as const,
    },
    {
      id: 'agents',
      label: 'Agent',
      value: String(snapshot.agents.length),
      hint: snapshot.activeAgentID ? `当前 ${snapshot.activeAgentID}` : '无当前',
      tone: snapshot.activeAgentID ? 'sky' as const : 'warn' as const,
      tab: 'agents' as const,
    },
    {
      id: 'personas',
      label: 'Persona',
      value: String(snapshot.personas.length),
      hint: activePersona || '未绑定',
      tone: 'neutral' as const,
      tab: 'personas' as const,
    },
    {
      id: 'plugins',
      label: '插件',
      value: String(snapshot.plugins.length),
      hint: `${Math.max(pluginsEnabled, pluginEnabledCount)} 启用`,
      tone: 'neutral' as const,
      tab: 'diagnostics' as const,
    },
    {
      id: 'issues',
      label: '配置问题',
      value: String(issueCount),
      hint: errorIssues > 0 ? `${errorIssues} 错误` : '含 warning',
      tone: issueCount > 0 ? 'warn' as const : 'ok' as const,
      tab: 'diagnostics' as const,
    },
    {
      id: 'usage',
      label: '近期请求',
      value: formatCompact(usageRequests),
      hint: usageErrors > 0 ? `${usageErrors} 错误 · ${formatCompact(usageTokens)} tok` : `${formatCompact(usageTokens)} tokens`,
      tone: usageErrors > 0 ? 'warn' as const : 'ok' as const,
      tab: 'usage' as const,
    },
  ];

  const recentIssues = snapshot.configIssues.slice(0, 6);
  const recentEvents = snapshot.usageEvents.slice(0, 8);
  const topComponents = [...snapshot.usageSummary]
    .sort((a, b) => numberField(b, 'request_count') - numberField(a, 'request_count'))
    .slice(0, 5);

  return {
    stats,
    health,
    recentIssues,
    recentEvents,
    topComponents,
    activeAgent,
    activePersona,
    memoryEnabled,
    usageRequests,
    usageErrors,
    usageTokens,
    pluginDiagStatus,
  };
}

function formatCompact(value: number) {
  if (!Number.isFinite(value)) return '0';
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`;
  if (value >= 10_000) return `${(value / 1000).toFixed(1)}k`;
  return String(Math.round(value));
}
