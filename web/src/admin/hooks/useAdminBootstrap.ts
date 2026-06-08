import { useEffect, useRef } from 'react';
import type { ProviderAdmin } from './useProviderAdmin';
import type { AgentAdmin } from './useAgentAdmin';
import type { PersonaAdmin } from './usePersonaAdmin';
import type { ChatSettingsAdmin } from './useChatSettingsAdmin';
import type { MemoryAdmin } from './useMemoryAdmin';
import type { SidecarAdmin } from './useSidecarAdmin';
import type { AdminStatusControls } from './useAdminStatus';
import type { TabID } from '../lib/adminData';

type BootstrapOptions = {
  providers: Pick<ProviderAdmin, 'reloadProviderPresets' | 'reloadProviders'>;
  agents: Pick<AgentAdmin, 'reloadAgents'>;
  personas: Pick<PersonaAdmin, 'reloadPersonas' | 'reloadProgressDefaults'>;
  chatSettings: Pick<ChatSettingsAdmin, 'reloadChatSettings'>;
  memory: Pick<MemoryAdmin, 'reloadEffectiveConfig' | 'reloadMemorySurfaces' | 'reloadConfigIssues' | 'reloadNaturalLatest'>;
  sidecar: Pick<SidecarAdmin, 'reloadSidecar'>;
  status: Pick<AdminStatusControls, 'showError'>;
};

export function useAdminBootstrap(activeTab: TabID, { providers, agents, personas, chatSettings, memory, sidecar, status }: BootstrapOptions) {
  const loadedResourcesRef = useRef(new Set<string>());
  const resourceRequestsRef = useRef(new Map<string, Promise<void>>());

  useEffect(() => {
    let cancelled = false;

    async function loadOnce(key: string, load: () => Promise<void>) {
      if (loadedResourcesRef.current.has(key)) return;
      const existing = resourceRequestsRef.current.get(key);
      if (existing) {
        await existing;
        return;
      }
      const request = Promise.resolve()
        .then(load)
        .then(() => {
          loadedResourcesRef.current.add(key);
        })
        .finally(() => {
          resourceRequestsRef.current.delete(key);
        });
      resourceRequestsRef.current.set(key, request);
      await request;
    }

    async function init() {
      try {
        const loadProviderBasics = () => Promise.all([
          loadOnce('provider-presets', providers.reloadProviderPresets),
          loadOnce('providers', () => providers.reloadProviders()),
        ]);
        const loadPersonaBasics = () => loadOnce('personas', () => personas.reloadPersonas());
        const loadAgentBasics = () => loadOnce('agents', () => agents.reloadAgents());
        const loadEffectiveConfig = () => loadOnce('effective-config', memory.reloadEffectiveConfig);

        switch (activeTab) {
          case 'providers':
            await loadProviderBasics();
            break;
          case 'agents':
            await Promise.all([loadProviderBasics(), loadPersonaBasics(), loadAgentBasics()]);
            break;
          case 'personas':
            await Promise.all([
              loadPersonaBasics(),
              loadOnce('progress-defaults', personas.reloadProgressDefaults),
              loadAgentBasics(),
            ]);
            break;
          case 'chat-settings':
            await loadOnce('chat-settings', chatSettings.reloadChatSettings);
            break;
          case 'memory-core':
            await Promise.all([
              loadEffectiveConfig(),
              loadOnce('memory-surfaces', memory.reloadMemorySurfaces),
              loadOnce('natural-latest', memory.reloadNaturalLatest),
            ]);
            break;
          case 'pipelines':
            await Promise.all([loadEffectiveConfig(), loadProviderBasics()]);
            break;
          case 'retrieval-mirror':
          case 'privacy-forget':
          case 'retention':
            await loadEffectiveConfig();
            break;
          case 'sidecar':
            await Promise.all([
              loadEffectiveConfig(),
              loadOnce('sidecar', sidecar.reloadSidecar),
            ]);
            break;
          case 'diagnostics':
            await Promise.all([
              loadEffectiveConfig(),
              loadOnce('config-issues', memory.reloadConfigIssues),
            ]);
            break;
        }
      } catch (error) {
        if (!cancelled) status.showError(error);
      }
    }
    init();
    return () => {
      cancelled = true;
    };
  }, [
    activeTab,
    providers.reloadProviderPresets,
    providers.reloadProviders,
    agents.reloadAgents,
    personas.reloadPersonas,
    personas.reloadProgressDefaults,
    chatSettings.reloadChatSettings,
    memory.reloadEffectiveConfig,
    memory.reloadMemorySurfaces,
    memory.reloadConfigIssues,
    memory.reloadNaturalLatest,
    sidecar.reloadSidecar,
    status.showError,
  ]);
}
