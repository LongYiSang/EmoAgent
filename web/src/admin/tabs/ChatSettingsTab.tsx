import { memo } from 'react';
import { boolField, pretty } from '../../shared/lib/data';
import type { ChatSettingsAdmin } from '../hooks/useChatSettingsAdmin';

export type ChatSettingsTabProps = ChatSettingsAdmin;

export default memo(function ChatSettingsTab({ chatSettings, reloadChatSettings, patchChatSettings, saveChatSettingsDraft }: ChatSettingsTabProps) {
  return (
    <div className="section">
      <div className="hero"><div><h2>聊天设置</h2><div className="meta">运行时聊天行为</div></div><div className="actions"><button className="btn ghost" id="reload-chat-settings" type="button" onClick={reloadChatSettings}>重新加载</button><button className="btn primary" id="save-chat-settings" type="button" onClick={saveChatSettingsDraft}>保存</button></div></div>
      <div className="grid">
        <label className="check"><input id="realtime-streaming" type="checkbox" checked={boolField(chatSettings, 'realtime_streaming')} onChange={event => patchChatSettings('realtime_streaming', event.target.checked)} /> 实时流式输出</label>
      </div>
      <pre className="code" id="chat-settings-json">{pretty(chatSettings)}</pre>
    </div>
  );
});
