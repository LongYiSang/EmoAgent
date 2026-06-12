import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { PluginsApp } from './PluginsApp';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <PluginsApp />
  </StrictMode>,
);