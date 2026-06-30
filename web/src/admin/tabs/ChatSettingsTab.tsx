import { memo } from 'react';
import type { AnyRecord } from '../../shared/lib/api';
import { boolField, field, pretty, stringField, toInt } from '../../shared/lib/data';
import type { ChatSettingsAdmin } from '../hooks/useChatSettingsAdmin';

export type ChatSettingsTabProps = ChatSettingsAdmin;

export default memo(function ChatSettingsTab({ chatSettings, reloadChatSettings, patchChatSettings, saveChatSettingsDraft }: ChatSettingsTabProps) {
  const router = field<AnyRecord>(chatSettings, 'prompt_router', {});
  const patchRouter = (key: string, value: unknown) => patchChatSettings('prompt_router', { ...router, [key]: value });

  return (
    <div className="section">
      <div className="hero"><div><h2>聊天设置</h2><div className="meta">运行时聊天行为</div></div><div className="actions"><button className="btn ghost" id="reload-chat-settings" type="button" onClick={reloadChatSettings}>重新加载</button><button className="btn primary" id="save-chat-settings" type="button" onClick={saveChatSettingsDraft}>保存</button></div></div>
      <div className="grid">
        <label className="check"><input id="realtime-streaming" type="checkbox" checked={boolField(chatSettings, 'realtime_streaming')} onChange={event => patchChatSettings('realtime_streaming', event.target.checked)} /> 实时流式输出</label>
        <div className="field">
          <label htmlFor="prompt-router-mode">Prompt Router</label>
          <select id="prompt-router-mode" value={stringField(router, 'mode') || 'auto'} onChange={event => patchRouter('mode', event.target.value)}>
            <option value="auto">自动判断</option>
            <option value="always_casual">始终普通聊天</option>
            <option value="always_work">始终工作模式</option>
          </select>
        </div>
        <div className="field"><label htmlFor="prompt-router-model">Router Model</label><input id="prompt-router-model" value={String(field(router, 'model', ''))} onChange={event => patchRouter('model', event.target.value)} /></div>
        <div className="field"><label htmlFor="prompt-router-sticky">Sticky Turns</label><input id="prompt-router-sticky" type="number" min="1" value={String(field(router, 'sticky_turns', ''))} onChange={event => patchRouter('sticky_turns', toInt(event.target.value))} /></div>
        <div className="field"><label htmlFor="prompt-router-context-turns">Context Turns</label><input id="prompt-router-context-turns" type="number" min="1" value={String(field(router, 'context_turns', ''))} onChange={event => patchRouter('context_turns', toInt(event.target.value))} /></div>
        <div className="field"><label htmlFor="prompt-router-context-chars">Context Chars</label><input id="prompt-router-context-chars" type="number" min="1" value={String(field(router, 'max_context_chars', ''))} onChange={event => patchRouter('max_context_chars', toInt(event.target.value))} /></div>
        <div className="field"><label htmlFor="prompt-router-timeout">Timeout MS</label><input id="prompt-router-timeout" type="number" min="1" value={String(field(router, 'timeout_ms', ''))} onChange={event => patchRouter('timeout_ms', toInt(event.target.value))} /></div>
        <div className="field"><label htmlFor="prompt-router-output">Max Output Tokens</label><input id="prompt-router-output" type="number" min="1" value={String(field(router, 'max_output_tokens', ''))} onChange={event => patchRouter('max_output_tokens', toInt(event.target.value))} /></div>
      </div>
      <pre className="code" id="chat-settings-json">{pretty(chatSettings)}</pre>
    </div>
  );
});
