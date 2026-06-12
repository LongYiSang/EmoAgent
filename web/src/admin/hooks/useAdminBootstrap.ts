import { useEffect, useRef } from 'react';
import type { ProviderAdmin } from './useProviderAdmin';
import type { AgentAdmin } from './useAgentAdmin';
import type { PersonaAdmin } from './usePersonaAdmin';
import type { ChatSettingsAdmin } from './useChatSettingsAdmin';
import type { MemoryAdmin } from './useMemoryAdmin';
import type { SidecarAdmin } from './useSidecarAdmin';
import type { AgentAffectAdmin } from './useAgentAffectAdmin';
import type { PluginAdmin } from './usePluginAdmin';
import type { AdminStatusControls } from './useAdminStatus';
import type { TabID } from '../lib/adminData';

type BootstrapOptions = {
  providers: Pick<ProviderAdmin, 'reloadProviderPresets' | 'reloadProviders'>;
  agents: Pick<AgentAdmin, 'reloadAgents'>;
  personas: Pick<PersonaAdmin, 'reloadPersonas' | 'reloadProgressDefaults'>;
  chatSettings: Pick<ChatSettingsAdmin, 'reloadChatSettings'>;
  memory: Pick<MemoryAdmin, 'reloadEffectiveConfig' | 'reloadMemorySurfaces' | 'reloadConfigIssues' | 'reloadNaturalLatest'>;
  agentAffect: Pick<AgentAffectAdmin, 'reloadAgentAffect'>;
  plugins: Pick<PluginAdmin, 'reloadPlugins'>;
  sidecar: Pick<SidecarAdmin, 'reloadSidecar'>;
  status: Pick<AdminStatusControls, 'showError'>;
};

export function useAdminBootstrap(activeTab: TabID, { providers, agents, personas, chatSettings, memory, agentAffect, plugins, sidecar, status }: BootstrapOptions) {
  const loadedResourcesRef = useRef(new Set<string>());
  const resourceRequestsRef = useRef(new Map<string, Promise<void>>());
  const loadersRef = useRef({ providers, agents, personas, chatSettings, memory, agentAffect, plugins, sidecar, status });
  loadersRef.current = { providers, agents, personas, chatSettings, memory, agentAffect, plugins, sidecar, status };

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
      const loaders = loadersRef.current;
      try {
        const loadProviderBasics = () => Promise.all([
          loadOnce('provider-presets', loaders.providers.reloadProviderPresets),
          loadOnce('providers', () => loaders.providers.reloadProviders()),
        ]);
        const loadPersonaBasics = () => loadOnce('personas', () => loaders.personas.reloadPersonas());
        const loadAgentBasics = () => loadOnce('agents', () => loaders.agents.reloadAgents());
        const loadEffectiveConfig = () => loadOnce('effective-config', loaders.memory.reloadEffectiveConfig);

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
              loadOnce('progress-defaults', loaders.personas.reloadProgressDefaults),
              loadAgentBasics(),
            ]);
            break;
          case 'chat-settings':
            await loadOnce('chat-settings', loaders.chatSettings.reloadChatSettings);
            break;
          case 'memory-core':
            await Promise.all([
              loadEffectiveConfig(),
              loadOnce('memory-surfaces', loaders.memory.reloadMemorySurfaces),
              loadOnce('natural-latest', loaders.memory.reloadNaturalLatest),
            ]);
            break;
          case 'agent-affect':
            await Promise.all([loadOnce('agent-affect', loaders.agentAffect.reloadAgentAffect), loadProviderBasics()]);
            break;
          case 'plugins':
            await loadOnce('plugins', loaders.plugins.reloadPlugins);
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
              loadOnce('sidecar', loaders.sidecar.reloadSidecar),
            ]);
            break;
          case 'diagnostics':
            await Promise.all([
              loadEffectiveConfig(),
              loadOnce('config-issues', loaders.memory.reloadConfigIssues),
            ]);
            break;
        }
      } catch (error) {
        if (!cancelled) loadersRef.current.status.showError(error);
      }
    }
    init();
    return () => {
      cancelled = true;
    };
  }, [activeTab]);
}
