import { lazy, Suspense, useCallback, useEffect, useMemo, useState } from 'react';
import { AppRail } from '../shared/components/AppRail';
import { ThemeToggle } from '../shared/components/ThemeToggle';
import { useTheme } from '../shared/hooks/useTheme';
import { classNames } from '../shared/lib/classNames';
import { useAdminStatus } from './hooks/useAdminStatus';
import { useProviderAdmin } from './hooks/useProviderAdmin';
import { useAgentAdmin } from './hooks/useAgentAdmin';
import { usePersonaAdmin } from './hooks/usePersonaAdmin';
import { useChatSettingsAdmin } from './hooks/useChatSettingsAdmin';
import { useMemoryAdmin } from './hooks/useMemoryAdmin';
import { useSidecarAdmin } from './hooks/useSidecarAdmin';
import { useAgentAffectAdmin } from './hooks/useAgentAffectAdmin';
import { usePromptCenterAdmin } from './hooks/usePromptCenterAdmin';
import { useWebSearchAdmin } from './hooks/useWebSearchAdmin';
import { usePlatformAdmin } from './hooks/usePlatformAdmin';
import { usePythonToolchainAdmin } from './hooks/usePythonToolchainAdmin';
import { useUsageAdmin } from './hooks/useUsageAdmin';
import { useOverviewAdmin } from './hooks/useOverviewAdmin';
import { useAdminBootstrap } from './hooks/useAdminBootstrap';
import { findAdminGroupForTab, findAdminTab, tabGroups, tabIDFromHash, type TabID } from './lib/adminData';
import '../styles.css';

function writeTabHash(id: TabID) {
  const next = `${window.location.pathname}${window.location.search}#${id}`;
  const current = `${window.location.pathname}${window.location.search}${window.location.hash}`;
  if (current !== next) history.replaceState(null, '', next);
}

const OverviewTab = lazy(() => import('./tabs/OverviewTab'));
const ProvidersTab = lazy(() => import('./tabs/ProvidersTab'));
const AgentsTab = lazy(() => import('./tabs/AgentsTab'));
const PersonasTab = lazy(() => import('./tabs/PersonasTab'));
const ChatSettingsTab = lazy(() => import('./tabs/ChatSettingsTab'));
const UsageTab = lazy(() => import('./tabs/UsageTab'));
const MemoryCoreTab = lazy(() => import('./tabs/MemoryCoreTab'));
const AgentAffectTab = lazy(() => import('./tabs/AgentAffectTab'));
const PromptCenterTab = lazy(() => import('./tabs/PromptCenterTab'));
const WebSearchPipelineTab = lazy(() => import('./tabs/WebSearchPipelineTab'));
const PlatformsTab = lazy(() => import('./tabs/PlatformsTab'));
const PythonToolchainTab = lazy(() => import('./tabs/PythonToolchainTab'));
const PipelinesTab = lazy(() => import('./tabs/PipelinesTab'));
const RetrievalTab = lazy(() => import('./tabs/RetrievalTab'));
const SidecarTab = lazy(() => import('./tabs/SidecarTab'));
const PrivacyForgetTab = lazy(() => import('./tabs/PrivacyForgetTab'));
const RetentionTab = lazy(() => import('./tabs/RetentionTab'));
const DiagnosticsTab = lazy(() => import('./tabs/DiagnosticsTab'));

export function AdminApp() {
  const { theme, toggleTheme } = useTheme();
  const [tab, setTabState] = useState<TabID>(() => tabIDFromHash());
  const status = useAdminStatus();
  const providers = useProviderAdmin(status);
  const agents = useAgentAdmin(status);
  const personas = usePersonaAdmin(status);
  const chatSettings = useChatSettingsAdmin(status);
  const memory = useMemoryAdmin(status);
  const sidecar = useSidecarAdmin(status);
  const agentAffect = useAgentAffectAdmin({ setStatus: status.setStatus, showError: status.showError, defaultPersonaID: agents.activePersona });
  const promptCenter = usePromptCenterAdmin(status);
  const webSearch = useWebSearchAdmin(status);
  const platforms = usePlatformAdmin(status);
  const pythonToolchain = usePythonToolchainAdmin(status);
  const usage = useUsageAdmin(status);
  const overview = useOverviewAdmin(status);

  const setTab = useCallback((id: TabID) => {
    setTabState(id);
    writeTabHash(id);
  }, []);

  useEffect(() => {
    writeTabHash(tab);
    const onHashChange = () => {
      const next = tabIDFromHash();
      setTabState(current => (current === next ? current : next));
    };
    window.addEventListener('hashchange', onHashChange);
    return () => window.removeEventListener('hashchange', onHashChange);
  }, []);

  useAdminBootstrap(tab, { providers, agents, personas, chatSettings, memory, agentAffect, promptCenter, webSearch, platforms, pythonToolchain, usage, sidecar, overview, status });

  const activeTab = useMemo(() => findAdminTab(tab), [tab]);
  const activeGroup = useMemo(() => findAdminGroupForTab(tab), [tab]);

  function renderActiveTab() {
    switch (tab) {
      case 'overview':
        return <OverviewTab {...overview} onNavigate={setTab} />;
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
            userAddressMigration={agents.userAddressMigration}
            reloadAgents={agents.reloadAgents}
            selectAgent={agents.selectAgent}
            patchAgentDraft={agents.patchAgentDraft}
            updateAgentPath={agents.updateAgentPath}
            replaceAgentDraft={agents.replaceAgentDraft}
            newAgent={agents.newAgent}
            submitAgent={agents.submitAgent}
            activateSelectedAgent={agents.activateSelectedAgent}
            deleteSelectedAgent={agents.deleteSelectedAgent}
            previewSelectedUserAddressMigration={agents.previewSelectedUserAddressMigration}
            executeSelectedUserAddressMigration={agents.executeSelectedUserAddressMigration}
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
      case 'usage':
        return <UsageTab {...usage} />;
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
      case 'prompt-center':
        return <PromptCenterTab {...promptCenter} agents={agents.agents} activeAgentID={agents.activeAgentID} />;
      case 'websearch-pipeline':
        return <WebSearchPipelineTab {...webSearch} />;
      case 'platforms':
        return <PlatformsTab {...platforms} agents={agents.agents} />;
      case 'python-toolchain':
        return <PythonToolchainTab {...pythonToolchain} />;
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
        return <DiagnosticsTab effectiveConfig={memory.effectiveConfig} configIssues={memory.configIssues} pluginDiagnostics={memory.pluginDiagnostics} reloadEffectiveConfig={memory.reloadEffectiveConfig} reloadConfigIssues={memory.reloadConfigIssues} reloadPluginDiagnostics={memory.reloadPluginDiagnostics} validateEffectiveConfig={memory.validateEffectiveConfig} />;
      default:
        return null;
    }
  }

  return (
    <div className="app-shell">
      <AppRail active="admin" />
      <main className="admin-page-wrap">
        <header className="admin-header">
          <div className="admin-header-text">
            {activeGroup ? <span className="admin-header-group">{activeGroup.label}</span> : null}
            <h1>{activeTab?.label || '管理配置'}</h1>
            <p>{activeTab?.description || '模型服务、Persona、记忆、Sidecar 与运行时生效配置'}</p>
          </div>
          <span className="status-chip"><span className="dot" /><span id="status">{status.status}</span></span>
          <ThemeToggle theme={theme} onToggle={toggleTheme} />
        </header>
        <div className="admin-body">
          <aside className="admin-tabs" aria-label="配置导航">
            {tabGroups.map(group => (
              <div className="admin-tab-group" key={group.id}>
                <div className="admin-tab-group-label">{group.label}</div>
                <div className="admin-tab-group-items">
                  {group.items.map(item => (
                    <button
                      className={classNames('admin-tab', tab === item.id && 'active')}
                      data-tab={item.id}
                      type="button"
                      key={item.id}
                      onClick={() => setTab(item.id)}
                    >
                      {item.label}
                    </button>
                  ))}
                </div>
              </div>
            ))}
          </aside>
          <section className="admin-content" key={tab}>
            <Suspense fallback={<div className="section"><div className="meta">加载中...</div></div>}>
              {renderActiveTab()}
            </Suspense>
          </section>
        </div>
      </main>
    </div>
  );
}
