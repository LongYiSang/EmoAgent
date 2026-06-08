import { type Dispatch, type SetStateAction, useCallback, useMemo, useState } from 'react';

export type AdminStatusControls = {
  status: string;
  setStatus: Dispatch<SetStateAction<string>>;
  showError: (error: unknown) => void;
};

export function useAdminStatus(): AdminStatusControls {
  const [status, setStatus] = useState('加载中...');

  const showError = useCallback((error: unknown) => {
    console.error(error);
    setStatus(error instanceof Error ? error.message : String(error));
  }, []);

  return useMemo(() => ({ status, setStatus, showError }), [status, showError]);
}
