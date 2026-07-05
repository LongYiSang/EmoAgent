import { useCallback, useMemo, useState } from 'react';
import type { AnyRecord } from '../../shared/lib/api';
import { field, pretty, stringField } from '../../shared/lib/data';
import { cloneRecord, setNestedValue } from '../lib/adminData';
import { loadConfigIssues, loadEffectiveConfig, loadPlatformStatus, savePlatformsConfig } from '../protocol/adminApi';
import type { AdminStatusControls } from './useAdminStatus';

const adapterID = 'qq-main';

type PlatformAdminOptions = Pick<AdminStatusControls, 'setStatus' | 'showError'>;

export function usePlatformAdmin({ setStatus, showError }: PlatformAdminOptions) {
  const [platformDraft, setPlatformDraft] = useState<AnyRecord>(() => defaultPlatformsDraft());
  const [platformStatus, setPlatformStatus] = useState<AnyRecord>({ enabled: false, adapters: [] });
  const [platformIssues, setPlatformIssues] = useState<AnyRecord[]>([]);

  const syncEffective = useCallback((effective: AnyRecord) => {
    setPlatformDraft(withQQMainDefaults(cloneRecord(field<AnyRecord>(effective, 'platforms', defaultPlatformsDraft()))));
    setPlatformIssues(platformIssueList(field<unknown>(effective, 'issues', [])));
  }, []);

  const reloadPlatformStatusAndIssues = useCallback(async () => {
    const [status, issues] = await Promise.all([loadPlatformStatus(), loadConfigIssues()]);
    setPlatformStatus(status);
    setPlatformIssues(platformIssueList(issues));
  }, []);

  const reloadPlatformAdmin = useCallback(async () => {
    setStatus('正在加载消息平台配置...');
    const [effective, status, issues] = await Promise.all([loadEffectiveConfig(), loadPlatformStatus(), loadConfigIssues()]);
    syncEffective(effective);
    setPlatformStatus(status);
    setPlatformIssues(platformIssueList(issues));
    setStatus('就绪');
  }, [reloadPlatformStatusAndIssues, setStatus, syncEffective]);

  const updatePlatformPath = useCallback((path: string[], value: unknown) => {
    setPlatformDraft(current => withQQMainDefaults(setNestedValue(current, path, value)));
  }, []);

  const savePlatformDraft = useCallback(async () => {
    setStatus('正在保存消息平台配置...');
    try {
      const effective = await savePlatformsConfig(stripAccessToken(withQQMainDefaults(platformDraft)));
      syncEffective(effective);
      await reloadPlatformStatusAndIssues();
      setStatus('消息平台配置已保存');
    } catch (error) {
      showError(error);
    }
  }, [platformDraft, reloadPlatformStatusAndIssues, setStatus, showError, syncEffective]);

  const testPlatformConnection = useCallback(async () => {
    setStatus('正在刷新消息平台状态...');
    try {
      await reloadPlatformStatusAndIssues();
      setStatus('消息平台状态已刷新');
    } catch (error) {
      showError(error);
    }
  }, [reloadPlatformStatusAndIssues, setStatus, showError]);

  const copySnowLumaConfig = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(buildSnowLumaConnectionText(platformDraft));
      setStatus('SnowLuma 连接信息已复制');
    } catch (error) {
      showError(error);
    }
  }, [platformDraft, setStatus, showError]);

  const copyEmoAgentYAML = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(buildEmoAgentYAML(platformDraft));
      setStatus('EmoAgent platforms 配置已复制');
    } catch (error) {
      showError(error);
    }
  }, [platformDraft, setStatus, showError]);

  return useMemo(() => ({
    platformDraft,
    platformStatus,
    platformIssues,
    platformJSON: pretty(platformDraft),
    adapterID,
    reloadPlatformAdmin,
    updatePlatformPath,
    savePlatformDraft,
    testPlatformConnection,
    copySnowLumaConfig,
    copyEmoAgentYAML,
  }), [
    platformDraft,
    platformStatus,
    platformIssues,
    reloadPlatformAdmin,
    updatePlatformPath,
    savePlatformDraft,
    testPlatformConnection,
    copySnowLumaConfig,
    copyEmoAgentYAML,
  ]);
}

function defaultPlatformsDraft(): AnyRecord {
  return {
    enabled: false,
    adapters: {
      [adapterID]: defaultAdapter(),
    },
  };
}

function defaultAdapter(): AnyRecord {
  return {
    enabled: true,
    kind: 'onebot_v11',
    instance_id: adapterID,
    platform_id: 'qq',
    config: {
      implementation: 'snowluma',
      source_type: 'onebot',
      transport: {
        mode: 'ws_reverse',
        reverse_path: `/api/platforms/onebot/v11/${adapterID}/ws`,
        access_token_env: 'SNOWLUMA_ONEBOT_TOKEN',
      },
      routing: {
        private_enabled: true,
        group_enabled: false,
      },
      message: {
        max_text_chars: 8000,
      },
      outbound: {
        max_message_chars: 1800,
      },
    },
  };
}

function withQQMainDefaults(platforms: AnyRecord): AnyRecord {
  const next = cloneRecord(platforms || {});
  if (!next.adapters || typeof next.adapters !== 'object' || Array.isArray(next.adapters)) next.adapters = {};
  const adapters = next.adapters as AnyRecord;
  adapters[adapterID] = mergeDefaults(field<AnyRecord>(adapters, adapterID, {}), defaultAdapter());
  return next;
}

function mergeDefaults(value: AnyRecord, defaults: AnyRecord): AnyRecord {
  const out = cloneRecord(value || {});
  for (const [key, defaultValue] of Object.entries(defaults)) {
    if (out[key] === undefined || out[key] === null || out[key] === '') {
      out[key] = defaultValue;
      continue;
    }
    if (isPlainRecord(out[key]) && isPlainRecord(defaultValue)) {
      out[key] = mergeDefaults(out[key] as AnyRecord, defaultValue as AnyRecord);
    }
  }
  return out;
}

function stripAccessToken(platforms: AnyRecord): AnyRecord {
  const next = cloneRecord(platforms);
  const adapter = field<AnyRecord>(field<AnyRecord>(next, 'adapters', {}), adapterID, {});
  const transport = field<AnyRecord>(field<AnyRecord>(adapter, 'config', {}), 'transport', {});
  delete transport.access_token;
  return next;
}

function platformIssueList(raw: unknown): AnyRecord[] {
  if (!Array.isArray(raw)) return [];
  return raw.filter(issue => String(field(issue, 'path', '')).startsWith('platforms')) as AnyRecord[];
}

function isPlainRecord(value: unknown): value is AnyRecord {
  return !!value && typeof value === 'object' && !Array.isArray(value);
}

function buildSnowLumaConnectionText(platforms: AnyRecord): string {
  const transport = currentTransport(platforms);
  const path = stringField(transport, 'reverse_path') || `/api/platforms/onebot/v11/${adapterID}/ws`;
  const tokenEnv = stringField(transport, 'access_token_env') || 'SNOWLUMA_ONEBOT_TOKEN';
  const wsURL = browserWSBase() + path;
  return [
    `Reverse WebSocket URL: ${wsURL}`,
    'Header:',
    '  X-Client-Role: Universal',
    '  X-Self-ID: <QQ_SELF_ID>',
    `  Authorization: Bearer <${tokenEnv}>`,
  ].join('\n');
}

function buildEmoAgentYAML(platforms: AnyRecord): string {
  const adapter = currentAdapter(platforms);
  const transport = currentTransport(platforms);
  const routing = field<AnyRecord>(field<AnyRecord>(adapter, 'config', {}), 'routing', {});
  const message = field<AnyRecord>(field<AnyRecord>(adapter, 'config', {}), 'message', {});
  const outbound = field<AnyRecord>(field<AnyRecord>(adapter, 'config', {}), 'outbound', {});
  return [
    'platforms:',
    `  enabled: ${Boolean(field(platforms, 'enabled', false))}`,
    '  adapters:',
    `    ${adapterID}:`,
    `      enabled: ${Boolean(field(adapter, 'enabled', true))}`,
    '      kind: onebot_v11',
    `      instance_id: ${yamlScalar(stringField(adapter, 'instance_id') || adapterID)}`,
    `      platform_id: ${yamlScalar(stringField(adapter, 'platform_id') || 'qq')}`,
    '      config:',
    '        implementation: snowluma',
    '        source_type: onebot',
    '        transport:',
    '          mode: ws_reverse',
    `          reverse_path: ${yamlScalar(stringField(transport, 'reverse_path') || `/api/platforms/onebot/v11/${adapterID}/ws`)}`,
    `          access_token_env: ${yamlScalar(stringField(transport, 'access_token_env') || 'SNOWLUMA_ONEBOT_TOKEN')}`,
    '        routing:',
    `          private_enabled: ${Boolean(field(routing, 'private_enabled', true))}`,
    `          group_enabled: ${Boolean(field(routing, 'group_enabled', false))}`,
    '        message:',
    `          max_text_chars: ${Number(field(message, 'max_text_chars', 8000)) || 8000}`,
    '        outbound:',
    `          max_message_chars: ${Number(field(outbound, 'max_message_chars', 1800)) || 1800}`,
  ].join('\n');
}

function currentAdapter(platforms: AnyRecord): AnyRecord {
  return field<AnyRecord>(field<AnyRecord>(platforms, 'adapters', {}), adapterID, defaultAdapter());
}

function currentTransport(platforms: AnyRecord): AnyRecord {
  return field<AnyRecord>(field<AnyRecord>(currentAdapter(platforms), 'config', {}), 'transport', {});
}

function browserWSBase() {
  if (typeof window === 'undefined') return 'ws://127.0.0.1:8080';
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${protocol}//${window.location.host}`;
}

function yamlScalar(value: string) {
  if (/^[A-Za-z0-9_./:-]+$/.test(value)) return value;
  return JSON.stringify(value);
}

export type PlatformAdmin = ReturnType<typeof usePlatformAdmin>;
