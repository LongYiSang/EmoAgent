import type { ContextStats } from '../protocol/wsTypes';

export type ContextStatsDisplayModel = {
  caption: string;
  label: string;
  title: string;
  percent: number;
};

export function contextStatsDisplayModel(stats: ContextStats): ContextStatsDisplayModel {
  const estimate = numberField(stats, 'estimated_input_tokens');
  const providerInput = numberField(stats, 'provider_input_tokens');
  const limit = numberField(stats, 'context_limit_tokens');
  const displayInput = providerInput > 0 ? providerInput : estimate;
  const source = providerInput > 0 ? '实际' : '估算';
  return {
    caption: '输入预算',
    label: displayInput > 0 && limit > 0 ? `${source} ${formatTokens(displayInput)} / ${formatTokens(limit)}` : '--',
    title: contextStatsTitle(stats),
    percent: contextStatsPercent(displayInput, limit),
  };
}

function contextStatsPercent(inputTokens: number, limitTokens: number): number {
  if (inputTokens <= 0 || limitTokens <= 0) return 0;
  return Math.max(1, Math.min(100, Math.round((inputTokens / limitTokens) * 100)));
}

function contextStatsTitle(stats: ContextStats): string {
  const estimate = numberField(stats, 'estimated_input_tokens');
  const providerInput = numberField(stats, 'provider_input_tokens');
  const providerOutput = numberField(stats, 'provider_output_tokens');
  const limit = numberField(stats, 'context_limit_tokens');
  const lines: string[] = [];
  if (providerInput > 0) {
    lines.push(`最终请求实际：${formatTokens(providerInput)} / ${formatTokens(limit)}`);
  }
  lines.push(`最终请求估算：${formatTokens(estimate)} / ${formatTokens(limit)}`);
  lines.push(`原始历史估算：${formatTokens(numberField(stats, 'raw_history_estimated_tokens'))}`);
  lines.push('分母：EmoAgent 输入预算，不是模型总上下文窗口');

  const compactReason = stringField(stats, 'compact_reason');
  if (compactReason) lines.push(`压缩原因：${compactReason}`);
  if (providerInput > 0 || providerOutput > 0) {
    lines.push(`Provider usage：${formatTokens(providerInput)} in / ${formatTokens(providerOutput)} out`);
  }
  const model = stringField(stats, 'model');
  if (model) lines.push(`模型：${model}`);
  return lines.join('\n');
}

function formatTokens(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '0';
  if (value >= 1000) return `${Math.round(value / 100) / 10}K`;
  return String(value);
}

function numberField(item: unknown, snake: string): number {
  const n = Number(field(item, snake, 0));
  return Number.isFinite(n) ? n : 0;
}

function stringField(item: unknown, snake: string): string {
  const value = field(item, snake, '');
  return typeof value === 'string' ? value : value == null ? '' : String(value);
}

function field<T>(item: unknown, snake: string, fallback: T): T {
  if (!item || typeof item !== 'object' || Array.isArray(item)) return fallback;
  const record = item as Record<string, unknown>;
  const pascal = snake.charAt(0).toUpperCase() + snake.slice(1);
  const camel = snake.replace(/_([a-z])/g, (_, c: string) => c.toUpperCase());
  const value = record[snake] ?? record[camel] ?? record[pascal];
  return value === undefined || value === null ? fallback : value as T;
}
