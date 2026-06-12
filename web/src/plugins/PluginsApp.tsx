import { useEffect, useRef } from 'react';
import { AppRail } from '../shared/components/AppRail';
import { useAdminStatus } from '../admin/hooks/useAdminStatus';
import { usePluginAdmin } from '../admin/hooks/usePluginAdmin';
import PluginsTab from '../admin/tabs/PluginsTab';
import '../styles.css';

export function PluginsApp() {
  const status = useAdminStatus();
  const plugins = usePluginAdmin(status);
  const didInitialLoadRef = useRef(false);

  useEffect(() => {
    if (didInitialLoadRef.current) return;
    didInitialLoadRef.current = true;
    plugins.reloadPlugins().catch(status.showError);
  }, [plugins.reloadPlugins, status.showError]);

  return (
    <div className="app-shell">
      <AppRail active="plugins" />
      <main className="admin-page-wrap">
        <header className="admin-header">
          <div>
            <h1>插件管理</h1>
            <p>安装、启用、审计与运行时插件</p>
          </div>
          <span className="status-chip"><span className="dot" /><span id="status">{status.status}</span></span>
        </header>
        <div style={{ padding: '18px', minHeight: 0, overflow: 'auto' }}>
          <PluginsTab {...plugins} />
        </div>
      </main>
    </div>
  );
}
