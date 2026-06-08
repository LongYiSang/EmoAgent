import { useCallback, useMemo, useState } from 'react';
import type { AnyRecord } from '../../shared/lib/api';
import { loadChatSettings, saveChatSettings } from '../protocol/adminApi';
import type { AdminStatusControls } from './useAdminStatus';

type ChatSettingsAdminOptions = Pick<AdminStatusControls, 'setStatus' | 'showError'>;

export function useChatSettingsAdmin({ setStatus, showError }: ChatSettingsAdminOptions) {
  const [chatSettings, setChatSettings] = useState<AnyRecord>({});

  const reloadChatSettings = useCallback(async () => {
    setChatSettings(await loadChatSettings());
  }, []);

  const patchChatSettings = useCallback((key: string, value: unknown) => {
    setChatSettings(current => ({ ...current, [key]: value }));
  }, []);

  const saveChatSettingsDraft = useCallback(async () => {
    try {
      const next = await saveChatSettings(chatSettings);
      setChatSettings(next);
      setStatus('聊天设置已保存');
    } catch (error) {
      showError(error);
    }
  }, [chatSettings, setStatus, showError]);

  return useMemo(() => ({
    chatSettings,
    reloadChatSettings,
    patchChatSettings,
    saveChatSettingsDraft,
  }), [chatSettings, reloadChatSettings, patchChatSettings, saveChatSettingsDraft]);
}

export type ChatSettingsAdmin = ReturnType<typeof useChatSettingsAdmin>;
