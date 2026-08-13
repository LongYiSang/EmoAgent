import { memo, useState } from 'react';
import type { AnyRecord } from '../../shared/lib/api';
import { boolField, field, pretty, stringField, toFloat, toInt } from '../../shared/lib/data';
import { classNames } from '../../shared/lib/classNames';
import type { ChatSettingsAdmin } from '../hooks/useChatSettingsAdmin';

export type ChatSettingsTabProps = ChatSettingsAdmin;

const COMMON_DELIMITERS = ['。', '！', '？', '～', '…', '\n', '!', '?'];

export default memo(function ChatSettingsTab({ chatSettings, reloadChatSettings, patchChatSettings, saveChatSettingsDraft }: ChatSettingsTabProps) {
  const [showRaw, setShowRaw] = useState(false);
  const [customDelim, setCustomDelim] = useState('');

  const router = field<AnyRecord>(chatSettings, 'prompt_router', {});
  const replyDelivery = field<AnyRecord>(chatSettings, 'reply_delivery', {});
  const replySegment = field<AnyRecord>(replyDelivery, 'segment', {});
  const replyTiming = field<AnyRecord>(replyDelivery, 'timing', {});

  const patchRouter = (key: string, value: unknown) => patchChatSettings('prompt_router', { ...router, [key]: value });
  const patchReplyDelivery = (key: string, value: unknown) => patchChatSettings('reply_delivery', { ...replyDelivery, [key]: value });
  const patchReplySegment = (key: string, value: unknown) => patchReplyDelivery('segment', { ...replySegment, [key]: value });
  const patchReplyTiming = (key: string, value: unknown) => patchReplyDelivery('timing', { ...replyTiming, [key]: value });

  // Split words (切分规则) - 富交互
  const splitWordsArr: string[] = Array.isArray(replySegment.split_words) ? (replySegment.split_words as string[]) : [];
  const splitMode = stringField(replySegment, 'split_mode') || 'natural';

  const toggleDelimiter = (token: string) => {
    const exists = splitWordsArr.includes(token);
    const next = exists ? splitWordsArr.filter(t => t !== token) : [...splitWordsArr, token];
    patchReplySegment('split_words', next);
  };

  const removeDelimiter = (token: string) => {
    patchReplySegment('split_words', splitWordsArr.filter(t => t !== token));
  };

  const addCustomDelimiter = () => {
    const raw = customDelim.trim();
    if (!raw) return;
    const token = raw === '\\n' ? '\n' : raw;
    if (!splitWordsArr.includes(token)) {
      patchReplySegment('split_words', [...splitWordsArr, token]);
    }
    setCustomDelim('');
  };

  const currentSplitWordsDisplay = splitWordsArr.map(t => ({
    token: t,
    label: t === '\n' ? '↵ 换行' : t,
  }));

  return (
    <div className="section chat-settings-tab">
      <div className="hero sticky-hero">
        <div>
          <div className="meta">流式行为、路由策略与分段输出节奏</div>
        </div>
        <div className="actions">
          <button className="btn ghost" type="button" onClick={reloadChatSettings}>重新加载</button>
          <button className="btn primary" type="button" onClick={saveChatSettingsDraft}>保存</button>
        </div>
      </div>

      {/* 基础流式行为 */}
      <div className="slot" style={{ marginBottom: 12 }}>
        <div className="slot-head"><strong>基础流式行为</strong></div>
        <div className="grid compact">
          <label className="check">
            <input type="checkbox" checked={boolField(chatSettings, 'realtime_streaming')} onChange={e => patchChatSettings('realtime_streaming', e.target.checked)} />
            实时流式输出
          </label>
          <label className="check">
            <input type="checkbox" checked={boolField(replyDelivery, 'disable_when_realtime_streaming')} onChange={e => patchReplyDelivery('disable_when_realtime_streaming', e.target.checked)} />
            实时流式时禁用分段
          </label>
        </div>
      </div>

      {/* Prompt Router */}
      <div className="slot" style={{ marginBottom: 12 }}>
        <div className="slot-head">
          <strong>提示词路由</strong>
          <span className="meta">在普通聊天与工作模式之间智能切换</span>
        </div>
        <div className="grid compact">
          <div className="field">
            <label>路由模式</label>
            <select value={stringField(router, 'mode') || 'auto'} onChange={e => patchRouter('mode', e.target.value)}>
              <option value="auto">自动判断</option>
              <option value="always_casual">始终普通聊天</option>
              <option value="always_work">始终工作模式</option>
            </select>
            <div className="meta">auto 会根据当前对话上下文自动决定使用哪种风格。</div>
          </div>
          <div className="field">
            <label>路由专用模型</label>
            <input value={String(field(router, 'model', ''))} onChange={e => patchRouter('model', e.target.value)} placeholder="留空则继承默认" />
          </div>
          <div className="field">
            <label>粘性轮次 (Sticky Turns)</label>
            <input type="number" min="1" value={String(field(router, 'sticky_turns', ''))} onChange={e => patchRouter('sticky_turns', toInt(e.target.value))} />
            <div className="meta">切换模式前至少保持多少轮对话。</div>
          </div>
          <div className="field">
            <label>上下文轮次数</label>
            <input type="number" min="1" value={String(field(router, 'context_turns', ''))} onChange={e => patchRouter('context_turns', toInt(e.target.value))} />
          </div>
          <div className="field">
            <label>最大上下文字符数</label>
            <input type="number" min="1" value={String(field(router, 'max_context_chars', ''))} onChange={e => patchRouter('max_context_chars', toInt(e.target.value))} />
          </div>
          <div className="field">
            <label>路由超时 (ms)</label>
            <input type="number" min="1" value={String(field(router, 'timeout_ms', ''))} onChange={e => patchRouter('timeout_ms', toInt(e.target.value))} />
          </div>
          <div className="field">
            <label>最大输出 Tokens</label>
            <input type="number" min="1" value={String(field(router, 'max_output_tokens', ''))} onChange={e => patchRouter('max_output_tokens', toInt(e.target.value))} />
          </div>
        </div>
      </div>

      {/* 分段投递 + 切分规则 + 输出间隔 */}
      <div className="slot">
        <div className="slot-head">
          <strong>分段投递</strong>
          <span className="meta">将长回复拆成多段逐步输出</span>
        </div>

        <div className="grid compact">
          <label className="check">
            <input type="checkbox" checked={boolField(replyDelivery, 'enabled')} onChange={e => patchReplyDelivery('enabled', e.target.checked)} />
            启用分段投递
          </label>
          <label className="check">
            <input type="checkbox" checked={boolField(replyTiming, 'enabled')} onChange={e => patchReplyTiming('enabled', e.target.checked)} />
            启用分段延迟
          </label>
        </div>

        {/* 内容保护 */}
        <div className="row-head" style={{ marginTop: 10 }}>
          <strong>内容保护</strong>
        </div>
        <div className="grid compact">
          <label className="check">
            <input type="checkbox" checked={boolField(replySegment, 'protect_code_blocks')} onChange={e => patchReplySegment('protect_code_blocks', e.target.checked)} />
            保护代码块
          </label>
          <label className="check">
            <input type="checkbox" checked={boolField(replySegment, 'protect_markdown_tables')} onChange={e => patchReplySegment('protect_markdown_tables', e.target.checked)} />
            保护 Markdown 表格
          </label>
          <label className="check">
            <input type="checkbox" checked={boolField(replySegment, 'protect_urls')} onChange={e => patchReplySegment('protect_urls', e.target.checked)} />
            保护 URL
          </label>
        </div>

        {/* 切分规则 - 重点美化新功能 */}
        <div style={{ marginTop: 14 }}>
          <div className="row-head">
            <strong>切分规则</strong>
            <span className="badge">切分</span>
          </div>

          {/* 模式切换器 */}
          <div style={{ display: 'flex', gap: 8, margin: '6px 0 4px' }}>
            <button
              type="button"
              className={classNames('btn', splitMode === 'natural' ? 'primary' : 'ghost', 'mini')}
              onClick={() => patchReplySegment('split_mode', 'natural')}
            >
              自然语言切分
            </button>
            <button
              type="button"
              className={classNames('btn', splitMode === 'regex' ? 'primary' : 'ghost', 'mini')}
              onClick={() => patchReplySegment('split_mode', 'regex')}
            >
              正则表达式切分
            </button>
          </div>
          <div className="meta" style={{ marginBottom: 6 }}>
            {splitMode === 'natural'
              ? '按句子结束符自然拆分，适合普通对话。'
              : '使用正则严格匹配切分点，适合需要精确控制的场景。'}
          </div>

          {/* 自然切分符 - 富交互 */}
          {splitMode === 'natural' && (
            <div className="field">
              <label>句子结束符 / 切分标记</label>

              {/* 当前激活的标记 chips */}
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, minHeight: 26, marginBottom: 4 }}>
                {currentSplitWordsDisplay.length > 0 ? (
                  currentSplitWordsDisplay.map(({ token, label }) => (
                    <span
                      key={token}
                      className="badge"
                      style={{ cursor: 'pointer', display: 'inline-flex', alignItems: 'center', gap: 3 }}
                      onClick={() => removeDelimiter(token)}
                      title="点击移除"
                    >
                      {label}
                      <span style={{ fontSize: 11, opacity: 0.65, marginLeft: 1 }}>×</span>
                    </span>
                  ))
                ) : (
                  <span className="meta">未自定义，将使用常见标点。</span>
                )}
              </div>

              {/* 快速添加 */}
              <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexWrap: 'wrap' }}>
                <span className="meta" style={{ marginRight: 2 }}>快速添加：</span>
                {COMMON_DELIMITERS.map((d) => {
                  const isActive = splitWordsArr.includes(d);
                  const disp = d === '\n' ? '↵' : d;
                  return (
                    <button
                      key={d}
                      type="button"
                      className={classNames('pill', isActive && 'active')}
                      style={{ fontSize: '12px', padding: '1px 7px', cursor: 'pointer' }}
                      onClick={() => toggleDelimiter(d)}
                    >
                      {disp}
                    </button>
                  );
                })}
              </div>

              {/* 自定义添加 */}
              <div style={{ display: 'flex', gap: 6, marginTop: 6, alignItems: 'center' }}>
                <input
                  value={customDelim}
                  onChange={(e) => setCustomDelim(e.target.value)}
                  onKeyDown={(e) => { if (e.key === 'Enter') addCustomDelimiter(); }}
                  placeholder="自定义字符（如 、 或 \\n）"
                  style={{ maxWidth: 180 }}
                />
                <button type="button" className="btn ghost mini" onClick={addCustomDelimiter}>添加</button>
              </div>
              <div className="meta">支持输入 \n 代表换行。</div>
            </div>
          )}

          {/* Regex 相关（自然模式下也显示，方便切换） */}
          <div className="grid compact" style={{ marginTop: 8 }}>
            <div className="field">
              <label>正则切分规则 (regex)</label>
              <input
                value={stringField(replySegment, 'regex')}
                onChange={e => patchReplySegment('regex', e.target.value)}
                placeholder="例如：[。！？\n]+"
              />
            </div>
            <div className="field">
              <label>切分后清理正则</label>
              <input
                value={stringField(replySegment, 'cleanup_regex')}
                onChange={e => patchReplySegment('cleanup_regex', e.target.value)}
                placeholder="可选，用于去除多余空白"
              />
            </div>
            <div className="field">
              <label>最大分段数</label>
              <input type="number" min="1" value={String(field(replySegment, 'max_segments', ''))} onChange={e => patchReplySegment('max_segments', toInt(e.target.value))} />
            </div>
            <div className="field">
              <label>长文本阈值（字符）</label>
              <input type="number" min="1" value={String(field(replySegment, 'long_text_threshold', ''))} onChange={e => patchReplySegment('long_text_threshold', toInt(e.target.value))} />
              <div className="meta">超过此长度才开始分段。</div>
            </div>
          </div>
        </div>

        {/* 分段输出间隔 - 新功能重点呈现 */}
        <div style={{ marginTop: 16 }}>
          <div className="row-head">
            <strong>分段输出间隔</strong>
            <span className="badge">节奏</span>
          </div>
          <div className="meta" style={{ marginBottom: 6 }}>
            控制每一段文字之间的停顿时间，让 AI 回复更有“思考”和“呼吸”的感觉。
          </div>

          <div className="grid compact">
            <div className="field">
              <label>最小延迟 (ms)</label>
              <input type="number" min="0" value={String(field(replyTiming, 'min_delay_ms', ''))} onChange={e => patchReplyTiming('min_delay_ms', toInt(e.target.value))} />
            </div>
            <div className="field">
              <label>最大延迟 (ms)</label>
              <input type="number" min="0" value={String(field(replyTiming, 'max_delay_ms', ''))} onChange={e => patchReplyTiming('max_delay_ms', toInt(e.target.value))} />
            </div>

            <div className="field">
              <label>随机间隔最小值 (ms)</label>
              <input type="number" min="0" value={String(field(replyTiming, 'random_interval_min_ms', ''))} onChange={e => patchReplyTiming('random_interval_min_ms', toInt(e.target.value))} />
            </div>
            <div className="field">
              <label>随机间隔最大值 (ms)</label>
              <input type="number" min="0" value={String(field(replyTiming, 'random_interval_max_ms', ''))} onChange={e => patchReplyTiming('random_interval_max_ms', toInt(e.target.value))} />
            </div>

            <div className="field">
              <label>对数缩放基准 (ms)</label>
              <input type="number" min="0" value={String(field(replyTiming, 'log_scale_ms', ''))} onChange={e => patchReplyTiming('log_scale_ms', toInt(e.target.value))} />
            </div>
            <div className="field">
              <label>对数底数</label>
              <input type="number" min="0.1" step="0.1" value={String(field(replyTiming, 'log_base', ''))} onChange={e => patchReplyTiming('log_base', toFloat(e.target.value))} />
              <div className="meta">用于长回复逐渐改变节奏的模型。</div>
            </div>
          </div>
        </div>
      </div>

      {/* 原始配置（可折叠） */}
      <div style={{ marginTop: 12 }}>
        <button
          type="button"
          className="btn ghost mini"
          onClick={() => setShowRaw(!showRaw)}
        >
          {showRaw ? '隐藏' : '显示'} 当前完整配置 JSON
        </button>
        {showRaw && (
          <pre className="code" id="chat-settings-json" style={{ marginTop: 8 }}>{pretty(chatSettings)}</pre>
        )}
      </div>
    </div>
  );
});
