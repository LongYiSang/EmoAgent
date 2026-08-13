import { useCallback, useDeferredValue, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import { AppRail } from '../shared/components/AppRail';
import { ThemeToggle } from '../shared/components/ThemeToggle';
import { useTheme } from '../shared/hooks/useTheme';
import { requestJSON } from '../shared/lib/api';
import { classNames } from '../shared/lib/classNames';
import '../styles.css';

type LogSourceType = 'main' | 'sidecar' | 'plugin';
type LogLevel = 'debug' | 'info' | 'warn' | 'error' | 'unknown';
type SourceStatus = 'active' | 'degraded' | 'unavailable';

type LogSource = {
  id: string;
  type: LogSourceType;
  label: string;
  status: SourceStatus;
  last_event_time?: string;
  last_error?: string;
};

type LogEvent = {
  id: string;
  time: string;
  source_type: LogSourceType;
  source_id: string;
  source_label?: string;
  level: LogLevel;
  message: string;
  fingerprint: string;
  attrs?: Record<string, string>;
  raw?: string;
};

type LogRow = LogEvent & {
  rowKey: string;
  repeat: number;
  displayText: string;
  firstId: string;
  lastId: string;
  firstTime: string;
  lastTime: string;
};

type SourcesResponse = { sources: LogSource[] };
type EventsResponse = { events: LogEvent[] };

const maxRows = 5000;
const maxSeenIds = 6000;
const defaultLimit = 500;
const levelOptions = [
  { value: '', label: 'ALL' },
  { value: 'debug', label: 'DEBUG+' },
  { value: 'info', label: 'INFO+' },
  { value: 'warn', label: 'WARN+' },
  { value: 'error', label: 'ERROR' },
];

export function LogsApp() {
  const { theme, toggleTheme } = useTheme();
  const [sources, setSources] = useState<LogSource[]>([]);
  const [sourceValue, setSourceValue] = useState('');
  const [minLevel, setMinLevel] = useState('');
  const [searchQuery, setSearchQuery] = useState('');
  const [autoScroll, setAutoScroll] = useState(true);
  const [wrap, setWrap] = useState(false);
  const [rows, setRows] = useState<LogRow[]>([]);
  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const [detailMode, setDetailMode] = useState<'parsed' | 'raw'>('parsed');
  const [streamStatus, setStreamStatus] = useState('连接中');
  const [error, setError] = useState<string | null>(null);
  const rowsRef = useRef<LogRow[]>([]);
  const seenIdsRef = useRef(new Set<string>());
  const seenOrderRef = useRef<string[]>([]);
  const lastEventIdRef = useRef('');
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const stickToBottomRef = useRef(true);
  const autoScrollRef = useRef(true);
  const pendingEventsRef = useRef<LogEvent[]>([]);
  const flushHandleRef = useRef<number | null>(null);
  const deferredSearchQuery = useDeferredValue(searchQuery);

  const sortedSources = useMemo(() => {
    return [...sources].sort((a, b) => sourceSortKey(a).localeCompare(sourceSortKey(b), 'zh-CN'));
  }, [sources]);
  const sourceByKey = useMemo(() => {
    return new Map(sources.map((source) => [sourceKey(source.type, source.id), source]));
  }, [sources]);
  const visibleRows = useMemo(() => filterLogRows(rows, deferredSearchQuery), [deferredSearchQuery, rows]);
  const selectedRow = useMemo(() => rows.find((row) => row.rowKey === selectedKey) ?? null, [rows, selectedKey]);
  const selectedJSON = useMemo(() => selectedRow ? detailJSON(selectedRow) : null, [selectedRow]);
  const searchActive = searchQuery.trim() !== '';

  const rowVirtualizer = useVirtualizer({
    count: visibleRows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => (wrap ? 66 : 42),
    overscan: 14,
  });

  useEffect(() => {
    rowVirtualizer.measure();
  }, [rowVirtualizer, wrap, visibleRows.length]);

  useEffect(() => {
    autoScrollRef.current = autoScroll;
    if (autoScroll) {
      stickToBottomRef.current = true;
    }
  }, [autoScroll]);

  const bottomKey = visibleRows.length === 0 ? '' : `${visibleRows[visibleRows.length - 1].rowKey}:${visibleRows[visibleRows.length - 1].repeat}:${visibleRows[visibleRows.length - 1].lastId}`;

  const scrollToBottom = useCallback(() => {
    if (visibleRows.length === 0) return;
    rowVirtualizer.scrollToIndex(visibleRows.length - 1, { align: 'end' });
    window.requestAnimationFrame(() => {
      const scroll = scrollRef.current;
      if (scroll && stickToBottomRef.current) {
        scroll.scrollTop = scroll.scrollHeight;
      }
    });
  }, [rowVirtualizer, visibleRows.length]);

  useLayoutEffect(() => {
    if (autoScroll && stickToBottomRef.current) {
      scrollToBottom();
    }
  }, [autoScroll, bottomKey, scrollToBottom, wrap]);

  useEffect(() => {
    if (selectedKey && !rows.some((row) => row.rowKey === selectedKey)) {
      setSelectedKey(null);
    }
  }, [rows, selectedKey]);

  const appendEvents = useCallback((events: LogEvent[], reset = false) => {
    stickToBottomRef.current = reset ? autoScrollRef.current : autoScrollRef.current && isNearBottom(scrollRef.current);
    if (reset) setSelectedKey(null);

    const seen = reset ? new Set<string>() : new Set(seenIdsRef.current);
    const seenOrder = reset ? [] : [...seenOrderRef.current];
    const next = reset ? [] : [...rowsRef.current];
    let lastEventID = reset ? '' : lastEventIdRef.current;

    for (const event of events) {
      if (!event.id || seen.has(event.id)) continue;
      const displayText = formatLogContent(event);
      rememberSeenID(event.id, seen, seenOrder);
      lastEventID = event.id;
      const last = next[next.length - 1];
      if (last && last.fingerprint === event.fingerprint) {
        next[next.length - 1] = {
          ...last,
          ...event,
          rowKey: last.rowKey,
          repeat: last.repeat + 1,
          displayText,
          firstId: last.firstId,
          firstTime: last.firstTime,
          lastId: event.id,
          lastTime: event.time,
        };
        continue;
      }
      next.push({
        ...event,
        rowKey: event.id,
        repeat: 1,
        displayText,
        firstId: event.id,
        lastId: event.id,
        firstTime: event.time,
        lastTime: event.time,
      });
    }

    if (next.length > maxRows) {
      next.splice(0, next.length - maxRows);
    }
    rowsRef.current = next;
    seenIdsRef.current = seen;
    seenOrderRef.current = seenOrder;
    lastEventIdRef.current = lastEventID;
    setRows(next);
  }, []);

  const cancelQueuedEvents = useCallback(() => {
    if (flushHandleRef.current !== null) {
      window.cancelAnimationFrame(flushHandleRef.current);
      flushHandleRef.current = null;
    }
    pendingEventsRef.current = [];
  }, []);

  const flushQueuedEvents = useCallback(() => {
    flushHandleRef.current = null;
    const events = pendingEventsRef.current;
    pendingEventsRef.current = [];
    if (events.length > 0) {
      appendEvents(events);
    }
  }, [appendEvents]);

  const queueEvent = useCallback((event: LogEvent) => {
    pendingEventsRef.current.push(event);
    if (flushHandleRef.current === null) {
      flushHandleRef.current = window.requestAnimationFrame(flushQueuedEvents);
    }
  }, [flushQueuedEvents]);

  const refreshSources = useCallback(async () => {
    const payload = await requestJSON<SourcesResponse>('/api/logs/sources');
    setSources(payload.sources ?? []);
  }, []);

  const reloadEvents = useCallback(async () => {
    cancelQueuedEvents();
    setError(null);
    setStreamStatus('加载中');
    const payload = await requestJSON<EventsResponse>(`/api/logs/events?${buildQuery(sourceValue, minLevel)}`);
    appendEvents(payload.events ?? [], true);
    setStreamStatus('已连接');
  }, [appendEvents, cancelQueuedEvents, minLevel, sourceValue]);

  useEffect(() => {
    let closed = false;
    refreshSources().catch((err) => {
      if (!closed) setError(errorMessage(err));
    });
    const timer = window.setInterval(() => {
      refreshSources().catch((err) => {
        if (!closed) setError(errorMessage(err));
      });
    }, 5000);
    return () => {
      closed = true;
      window.clearInterval(timer);
    };
  }, [refreshSources]);

  useEffect(() => {
    let eventSource: EventSource | null = null;
    let closed = false;

    async function connect() {
      try {
        cancelQueuedEvents();
        setError(null);
        setStreamStatus('加载中');
        const initial = await requestJSON<EventsResponse>(`/api/logs/events?${buildQuery(sourceValue, minLevel)}`);
        if (closed) return;
        appendEvents(initial.events ?? [], true);
        const afterID = lastEventIdRef.current;
        eventSource = new EventSource(`/api/logs/stream?${buildQuery(sourceValue, minLevel, afterID)}`);
        eventSource.addEventListener('open', () => {
          if (!closed) setStreamStatus('已连接');
        });
        eventSource.addEventListener('error', () => {
          if (!closed) setStreamStatus('重连中');
        });
        eventSource.addEventListener('log', (event) => {
          try {
            queueEvent(JSON.parse((event as MessageEvent).data) as LogEvent);
          } catch {
            if (!closed) setError('日志流数据解析失败');
          }
        });
      } catch (err) {
        if (!closed) {
          setError(errorMessage(err));
          setStreamStatus('连接失败');
        }
      }
    }

    connect();
    return () => {
      closed = true;
      cancelQueuedEvents();
      eventSource?.close();
    };
  }, [appendEvents, cancelQueuedEvents, minLevel, queueEvent, sourceValue]);

  const clearRows = useCallback(() => {
    cancelQueuedEvents();
    seenIdsRef.current.clear();
    seenOrderRef.current = [];
    lastEventIdRef.current = '';
    stickToBottomRef.current = autoScrollRef.current;
    rowsRef.current = [];
    setRows([]);
    setSelectedKey(null);
  }, [cancelQueuedEvents]);

  const exportVisibleRows = useCallback(() => {
    if (visibleRows.length === 0) return;
    const lines = visibleRows.map((row) => {
      const repeat = row.repeat > 1 ? ` *${row.repeat}` : '';
      return `${row.lastTime} ${row.level.toUpperCase().padEnd(7)} [${sourceLabel(row, sourceByKey)}] ${row.displayText}${repeat}`;
    });
    const blob = new Blob([`${lines.join('\n')}\n`], { type: 'text/plain;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `emoagent-logs-${new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19)}.log`;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    URL.revokeObjectURL(url);
  }, [sourceByKey, visibleRows]);

  const toggleAutoScroll = useCallback(() => {
    setAutoScroll((enabled) => {
      const next = !enabled;
      stickToBottomRef.current = next;
      return next;
    });
  }, []);

  const handleScroll = useCallback(() => {
    stickToBottomRef.current = autoScrollRef.current && isNearBottom(scrollRef.current);
  }, []);

  return (
    <div className="app-shell">
      <AppRail active="logs" />
      <main className="logs-page-wrap">
        <header className="admin-header">
          <div>
            <h1>日志中心</h1>
            <p>运行日志聚合流</p>
          </div>
          <div className="logs-header-meta">
            <span>{visibleRows.length}/{rows.length} 条</span>
            {searchActive && <span>搜索中</span>}
          </div>
          <span className={classNames('status-chip', streamStatus !== '已连接' && 'warn')}>
            <span className="dot" />
            <span>{streamStatus}</span>
          </span>
          <ThemeToggle theme={theme} onToggle={toggleTheme} />
        </header>

        <section className="logs-toolbar" aria-label="日志筛选">
          <label className="logs-search">
            <span className="sr-only">搜索日志</span>
            <input
              value={searchQuery}
              onChange={(event) => setSearchQuery(event.target.value)}
              placeholder="搜索 message / raw / source"
              type="search"
            />
            {searchActive && (
              <button aria-label="清除搜索" title="清除搜索" type="button" onClick={() => setSearchQuery('')}>x</button>
            )}
          </label>
          <label className="logs-source-field">
            <span className="sr-only">来源</span>
            <select value={sourceValue} onChange={(event) => setSourceValue(event.target.value)}>
              <option value="">全部来源</option>
              {sortedSources.map((source) => (
                <option key={sourceKey(source.type, source.id)} value={sourceKey(source.type, source.id)}>
                  {source.label || source.id} · {sourceStatusText(source.status)}
                </option>
              ))}
            </select>
          </label>
          <div className="logs-level-filter" role="group" aria-label="日志级别">
            {levelOptions.map((option) => (
              <button
                aria-pressed={minLevel === option.value}
                className={classNames(minLevel === option.value && 'active')}
                key={option.value || 'all'}
                type="button"
                onClick={() => setMinLevel(option.value)}
              >
                {option.label}
              </button>
            ))}
          </div>
          <div className="logs-tool-actions">
            <button aria-pressed={autoScroll} className={classNames('log-tool-button', autoScroll && 'active')} title="跟随滚动" type="button" onClick={toggleAutoScroll}>↓<span className="sr-only">跟随滚动</span></button>
            <button aria-pressed={wrap} className={classNames('log-tool-button', wrap && 'active')} title="自动换行" type="button" onClick={() => setWrap((value) => !value)}>↩<span className="sr-only">自动换行</span></button>
            <button className="log-tool-button" title="重新加载" type="button" onClick={() => void reloadEvents()}>↻<span className="sr-only">重新加载</span></button>
            <button className="log-tool-button" disabled={visibleRows.length === 0} title="导出当前视图" type="button" onClick={exportVisibleRows}>⇩<span className="sr-only">导出当前视图</span></button>
            <button className="log-tool-button danger" title="清空显示" type="button" onClick={clearRows}>×<span className="sr-only">清空显示</span></button>
          </div>
        </section>

        <section className="logs-content">
          {error && <div className="logs-error">{error}</div>}
          <div className="logs-source-strip" aria-label="日志来源状态">
            <button className={classNames('log-source-pill', sourceValue === '' && 'active')} type="button" onClick={() => setSourceValue('')}>
              全部 <span>{rows.length}</span>
            </button>
            {sortedSources.map((source) => {
              const key = sourceKey(source.type, source.id);
              return (
                <button className={classNames('log-source-pill', source.status, sourceValue === key && 'active')} type="button" key={key} onClick={() => setSourceValue(key)}>
                  {source.label || source.id}
                  <span>{sourceStatusText(source.status)}</span>
                </button>
              );
            })}
          </div>

          <div className={classNames('log-stream', wrap && 'wrap')}>
            <div className="log-stream-head">
              <span>时间</span>
              <span>级别</span>
              <span>来源</span>
              <span>内容</span>
              <span>重复</span>
            </div>
            <div className="log-scroll" ref={scrollRef} onScroll={handleScroll}>
              {visibleRows.length === 0 ? (
                <div className="logs-empty">
                  {rows.length === 0 ? '暂无日志' : '没有符合筛选条件的日志'}
                  {searchActive && <button type="button" onClick={() => setSearchQuery('')}>清除搜索</button>}
                </div>
              ) : (
                <div className="log-virtual" style={{ height: `${rowVirtualizer.getTotalSize()}px` }}>
                  {rowVirtualizer.getVirtualItems().map((virtualRow) => {
                    const row = visibleRows[virtualRow.index];
                    return (
                      <button
                        className={classNames('log-row', `level-${row.level}`, selectedKey === row.rowKey && 'active')}
                        data-index={virtualRow.index}
                        key={row.rowKey}
                        ref={rowVirtualizer.measureElement}
                        style={{ transform: `translateY(${virtualRow.start}px)` }}
                        type="button"
                        onClick={() => setSelectedKey(row.rowKey)}
                      >
                        <span className="log-time">{formatTime(row.lastTime)}</span>
                        <span className="log-level">{row.level}</span>
                        <span className="log-source">{sourceLabel(row, sourceByKey)}</span>
                        <span className="log-message">{row.displayText}</span>
                        <span className="log-repeat">{row.repeat > 1 ? `*${row.repeat}` : ''}</span>
                      </button>
                    );
                  })}
                </div>
              )}
            </div>
          </div>

          {selectedRow && (
            <div className={classNames('log-detail', wrap && 'wrap')}>
              <div className="log-detail-head">
                <div>
                  <b>{sourceLabel(selectedRow, sourceByKey)}</b>
                  <span>{selectedRow.level} · {formatTime(selectedRow.firstTime)}{selectedRow.repeat > 1 ? ` - ${formatTime(selectedRow.lastTime)}` : ''}</span>
                </div>
                <button aria-label="关闭详情" className="log-detail-close" title="关闭详情" type="button" onClick={() => setSelectedKey(null)}>×</button>
              </div>
              <div className="log-detail-actions">
                <button className="btn ghost mini" type="button" onClick={() => setSourceValue(sourceKey(selectedRow.source_type, selectedRow.source_id))}>只看此来源</button>
                {selectedJSON && (
                  <button
                    aria-label="切换日志详情格式"
                    className="btn ghost mini log-detail-mode-button"
                    type="button"
                    onClick={() => setDetailMode((mode) => (mode === 'parsed' ? 'raw' : 'parsed'))}
                  >
                    {detailMode === 'parsed' ? '原始' : '解析'}
                  </button>
                )}
                {selectedRow.repeat > 1 && <span className="badge active">*{selectedRow.repeat}</span>}
              </div>
              <div className="kv">
                <span>ID</span><b>{selectedRow.firstId === selectedRow.lastId ? selectedRow.firstId : `${selectedRow.firstId} - ${selectedRow.lastId}`}</b>
                <span>来源</span><b>{selectedRow.source_type}/{selectedRow.source_id}</b>
                <span>指纹</span><b>{selectedRow.fingerprint}</b>
              </div>
              {selectedJSON ? (
                detailMode === 'parsed' ? (
                  <div className="log-json-table">
                    {Object.entries(selectedJSON.value).map(([key, value]) => (
                      <div className="log-json-row" key={key}>
                        <span>{key}</span>
                        <pre>{formatJSONValue(value)}</pre>
                      </div>
                    ))}
                  </div>
                ) : (
                  <pre className="log-detail-code">{selectedJSON.raw}</pre>
                )
              ) : (
                <pre className="log-detail-code">{selectedRow.displayText}</pre>
              )}
            </div>
          )}
        </section>
      </main>
    </div>
  );
}

function rememberSeenID(id: string, seen: Set<string>, order: string[]) {
  seen.add(id);
  order.push(id);
  while (order.length > maxSeenIds) {
    const oldest = order.shift();
    if (oldest) seen.delete(oldest);
  }
}

function buildQuery(sourceValue: string, minLevel: string, afterID = '') {
  const params = new URLSearchParams();
  params.set('limit', String(defaultLimit));
  const [sourceType, sourceID] = splitSourceValue(sourceValue);
  if (sourceType && sourceID) {
    params.set('source_type', sourceType);
    params.set('source_id', sourceID);
  }
  if (minLevel) params.set('min_level', minLevel);
  if (afterID) params.set('after_id', afterID);
  return params.toString();
}

function splitSourceValue(value: string): [string, string] {
  const index = value.indexOf('/');
  if (index < 0) return ['', ''];
  return [value.slice(0, index), value.slice(index + 1)];
}

function sourceKey(type: string, id: string) {
  return `${type}/${id}`;
}

function sourceSortKey(source: LogSource) {
  const typeOrder = source.type === 'main' ? '0' : source.type === 'sidecar' ? '1' : '2';
  return `${typeOrder}:${source.label || source.id}`;
}

function sourceLabel(event: LogEvent, sources: Map<string, LogSource>) {
  const source = sources.get(sourceKey(event.source_type, event.source_id));
  return source?.label || event.source_label || event.source_id;
}

function sourceStatusText(status: SourceStatus) {
  switch (status) {
    case 'active':
      return '可用';
    case 'degraded':
      return '异常';
    case 'unavailable':
      return '不可用';
  }
}

function formatTime(value: string) {
  if (!value) return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleTimeString('zh-CN', { hour12: false });
}

function formatLogContent(event: LogEvent) {
  if (event.raw) return event.raw;
  const message = event.message || '';
  const attrs = Object.entries(event.attrs ?? {}).sort(([left], [right]) => left.localeCompare(right));
  if (attrs.length === 0) return message;
  const suffix = attrs.map(([key, value]) => `${key}=${quoteAttrValue(value)}`).join(' ');
  return message ? `${message} ${suffix}` : suffix;
}

function filterLogRows(rows: LogRow[], query: string) {
  const keyword = query.trim().toLowerCase();
  if (!keyword) return rows;
  return rows.filter((row) => {
    const attrs = row.attrs ? JSON.stringify(row.attrs) : '';
    return [
      row.displayText,
      row.raw,
      row.message,
      row.source_label,
      row.source_id,
      attrs,
    ].some((value) => String(value ?? '').toLowerCase().includes(keyword));
  });
}

function detailJSON(event: LogEvent) {
  if (event.attrs && Object.keys(event.attrs).length > 0) {
    return { value: event.attrs as Record<string, unknown>, raw: JSON.stringify(event.attrs, null, 2) };
  }
  const raw = event.raw?.trim();
  if (!raw || (raw[0] !== '{' && raw[0] !== '[')) return null;
  try {
    const parsed = JSON.parse(raw) as unknown;
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      return { value: parsed as Record<string, unknown>, raw: event.raw };
    }
  } catch {
    return null;
  }
  return null;
}

function formatJSONValue(value: unknown) {
  if (typeof value === 'string') return value;
  if (value == null || typeof value === 'number' || typeof value === 'boolean') return String(value);
  return JSON.stringify(value, null, 2);
}

function quoteAttrValue(value: string) {
  if (!/\s/.test(value)) return value;
  return JSON.stringify(value);
}

function errorMessage(err: unknown) {
  return err instanceof Error ? err.message : '日志中心请求失败';
}

function isNearBottom(element: HTMLElement | null) {
  if (!element) return true;
  return element.scrollHeight - element.scrollTop - element.clientHeight < 80;
}
