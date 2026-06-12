import { lazy, Suspense, useState } from 'react';
import { AppRail } from '../shared/components/AppRail';
import { classNames } from '../shared/lib/classNames';
import { useAdminStatus } from './hooks/useAdminStatus';
import { useProviderAdmin } from './hooks/useProviderAdmin';
import { useAgentAdmin } from './hooks/useAgentAdmin';
import { usePersonaAdmin } from './hooks/usePersonaAdmin';
import { useChatSettingsAdmin } from './hooks/useChatSettingsAdmin';
import { useMemoryAdmin } from './hooks/useMemoryAdmin';
import { useSidecarAdmin } from './hooks/useSidecarAdmin';
import { useAgentAffectAdmin } from './hooks/useAgentAffectAdmin';
import { usePluginAdmin } from './hooks/usePluginAdmin';
import { useAdminBootstrap } from './hooks/useAdminBootstrap';
import { tabs, type TabID } from './lib/adminData';
import '../styles.css';

const ProvidersTab = lazy(() => import('./tabs/ProvidersTab'));
const AgentsTab = lazy(() => import('./tabs/AgentsTab'));
const PersonasTab = lazy(() => import('./tabs/PersonasTab'));
const ChatSettingsTab = lazy(() => import('./tabs/ChatSettingsTab'));
const MemoryCoreTab = lazy(() => import('./tabs/MemoryCoreTab'));
const AgentAffectTab = lazy(() => import('./tabs/AgentAffectTab'));
const PluginsTab = lazy(() => import('./tabs/PluginsTab'));
const PipelinesTab = lazy(() => import('./tabs/PipelinesTab'));
const RetrievalTab = lazy(() => import('./tabs/RetrievalTab'));
const SidecarTab = lazy(() => import('./tabs/SidecarTab'));
const PrivacyForgetTab = lazy(() => import('./tabs/PrivacyForgetTab'));
const RetentionTab = lazy(() => import('./tabs/RetentionTab'));
const DiagnosticsTab = lazy(() => import('./tabs/DiagnosticsTab'));

export function AdminApp() {
  const [tab, setTab] = useState<TabID>('providers');
  const status = useAdminStatus();
  const providers = useProviderAdmin(status);
  const agents = useAgentAdmin(status);
  const personas = usePersonaAdmin(status);
  const chatSettings = useChatSettingsAdmin(status);
  const memory = useMemoryAdmin(status);
  const sidecar = useSidecarAdmin(status);
  const agentAffect = useAgentAffectAdmin(status);
  const plugins = usePluginAdmin(status);

  useAdminBootstrap(tab, { providers, agents, personas, chatSettings, memory, agentAffect, plugins, sidecar, status });

  function renderActiveTab() {
    switch (tab) {
      case 'providers':
        return (
          <ProvidersTab
            providerPresets={providers.providerPresets}
            providers={providers.providers}
            providerModels={providers.providerModels}
            providerEnv={providers.providerEnv}
            selectedProvider={providers.selectedProvider}
            providerDraft={providers.providerDraft}
            reloadProviders={providers.reloadProviders}
            selectProvider={providers.selectProvider}
            patchProviderDraft={providers.patchProviderDraft}
            setProviderCapability={providers.setProviderCapability}
            applyProviderPreset={providers.applyProviderPreset}
            newProvider={providers.newProvider}
            submitProvider={providers.submitProvider}
            refreshSelectedProviderModels={providers.refreshSelectedProviderModels}
            testSelectedProvider={providers.testSelectedProvider}
            deleteSelectedProvider={providers.deleteSelectedProvider}
          />
        );
      case 'agents':
        return (
          <AgentsTab
            agents={agents.agents}
            activeAgentID={agents.activeAgentID}
            selectedAgent={agents.selectedAgent}
            agentDraft={agents.agentDraft}
            reloadAgents={agents.reloadAgents}
            selectAgent={agents.selectAgent}
            patchAgentDraft={agents.patchAgentDraft}
            updateAgentPath={agents.updateAgentPath}
            replaceAgentDraft={agents.replaceAgentDraft}
            newAgent={agents.newAgent}
            submitAgent={agents.submitAgent}
            activateSelectedAgent={agents.activateSelectedAgent}
            deleteSelectedAgent={agents.deleteSelectedAgent}
            providers={providers.providers}
            providerPresets={providers.providerPresets}
            modelOptions={providers.modelOptions}
            personas={personas.personas}
          />
        );
      case 'personas':
        return (
          <PersonasTab
            personas={personas.personas}
            selectedPersona={personas.selectedPersona}
            personaDraft={personas.personaDraft}
            progressDraft={personas.progressDraft}
            progressDraftJSON={personas.progressDraftJSON}
            progressDraftError={personas.progressDraftError}
            progressDefaults={personas.progressDefaults}
            reloadPersonas={personas.reloadPersonas}
            selectPersona={personas.selectPersona}
            patchPersonaDraft={personas.patchPersonaDraft}
            patchProgressDraftJSON={personas.patchProgressDraftJSON}
            newPersona={personas.newPersona}
            submitPersona={personas.submitPersona}
            deleteSelectedPersona={personas.deleteSelectedPersona}
            activePersona={agents.activePersona}
          />
        );
      case 'chat-settings':
        return <ChatSettingsTab {...chatSettings} />;
      case 'memory-core':
        return (
          <MemoryCoreTab
            effectiveConfig={memory.effectiveConfig}
            memoryDraft={memory.memoryDraft}
            memoryFeatures={memory.memoryFeatures}
            memoryJobs={memory.memoryJobs}
            memorySegments={memory.memorySegments}
            naturalMemoryLatest={memory.naturalMemoryLatest}
            reloadMemorySurfaces={memory.reloadMemorySurfaces}
            reloadNaturalLatest={memory.reloadNaturalLatest}
            runNaturalMemoryNow={memory.runNaturalMemoryNow}
            saveMemoryCore={memory.saveMemoryCore}
            saveMemoryFeaturesDraft={memory.saveMemoryFeaturesDraft}
            patchMemoryDraft={memory.patchMemoryDraft}
            updateMemoryPath={memory.updateMemoryPath}
          />
        );
      case 'agent-affect':
        return <AgentAffectTab {...agentAffect} providers={providers.providers} modelOptions={providers.modelOptions} />;
      case 'plugins':
        return <PluginsTab {...plugins} />;
      case 'pipelines':
        return <PipelinesTab providers={providers.providers} memoryDraft={memory.memoryDraft} updateMemoryPath={memory.updateMemoryPath} savePipelines={memory.savePipelines} />;
      case 'retrieval-mirror':
        return <RetrievalTab memoryDraft={memory.memoryDraft} effectiveConfig={memory.effectiveConfig} updateMemoryPath={memory.updateMemoryPath} saveRetrieval={memory.saveRetrieval} />;
      case 'sidecar':
        return <SidecarTab memoryDraft={memory.memoryDraft} updateMemoryPath={memory.updateMemoryPath} saveSidecarConfig={memory.saveSidecarConfig} {...sidecar} />;
      case 'privacy-forget':
        return <PrivacyForgetTab memoryDraft={memory.memoryDraft} effectiveConfig={memory.effectiveConfig} privacyDraft={memory.privacyDraft} setPrivacyDraft={memory.setPrivacyDraft} savePrivacyForget={memory.savePrivacyForget} />;
      case 'retention':
        return <RetentionTab effectiveConfig={memory.effectiveConfig} retentionDraft={memory.retentionDraft} setRetentionDraft={memory.setRetentionDraft} saveRetention={memory.saveRetention} />;
      case 'diagnostics':
        return <DiagnosticsTab effectiveConfig={memory.effectiveConfig} configIssues={memory.configIssues} reloadEffectiveConfig={memory.reloadEffectiveConfig} reloadConfigIssues={memory.reloadConfigIssues} validateEffectiveConfig={memory.validateEffectiveConfig} />;
      default:
        return null;
    }
  }

  return (
    <div className="app-shell">
      <AppRail active="admin" />
      <main className="admin-page-wrap">
        <header className="admin-header">
          <div>
            <h1>管理配置</h1>
            <p>模型服务、Persona、记忆、Sidecar 与运行时生效配置</p>
          </div>
          <span className="status-chip"><span className="dot" /><span id="status">{status.status}</span></span>
        </header>
        <div className="admin-body">
          <aside className="admin-tabs">
            {tabs.map(item => <button className={classNames('admin-tab', tab === item.id && 'active')} data-tab={item.id} type="button" key={item.id} onClick={() => setTab(item.id)}>{item.label}</button>)}
          </aside>
          <section className="admin-content">
            <Suspense fallback={<div className="section">加载中...</div>}>
              {renderActiveTab()}
            </Suspense>
          </section>
        </div>
      </main>
    </div>
  );
}
