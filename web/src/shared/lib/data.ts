import type { AnyRecord } from './api';

export function isRecord(value: unknown): value is AnyRecord {
  return !!value && typeof value === 'object' && !Array.isArray(value);
}

export function field<T = unknown>(item: unknown, snake: string, fallback: T): T {
  if (!isRecord(item)) return fallback;
  const pascal = snake.charAt(0).toUpperCase() + snake.slice(1);
  const camel = snake.replace(/_([a-z])/g, (_, c: string) => c.toUpperCase());
  const value = item[snake] ?? item[camel] ?? item[pascal];
  return value === undefined || value === null ? fallback : value as T;
}

export function stringField(item: unknown, snake: string): string {
  const value = field<unknown>(item, snake, '');
  return typeof value === 'string' ? value : value == null ? '' : String(value);
}

export function numberField(item: unknown, snake: string): number {
  const value = field<unknown>(item, snake, 0);
  const n = Number(value);
  return Number.isFinite(n) ? n : 0;
}

export function boolField(item: unknown, snake: string): boolean {
  return Boolean(field<unknown>(item, snake, false));
}

export function arrayField<T = unknown>(item: unknown, snake: string): T[] {
  const value = field<unknown>(item, snake, []);
  return Array.isArray(value) ? value as T[] : [];
}

export function parseMaybeJSON(value: unknown): AnyRecord {
  if (isRecord(value)) return value;
  if (typeof value !== 'string' || !value.trim()) return {};
  try {
    const parsed = JSON.parse(value);
    return isRecord(parsed) ? parsed : {};
  } catch {
    return {};
  }
}

export function formatTime(raw: unknown): string {
  if (!raw) return 'Unknown time';
  const date = new Date(String(raw));
  if (Number.isNaN(date.getTime())) return String(raw);
  return date.toLocaleString('zh-CN', {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

export function timelineMillis(raw: unknown, fallback = Date.now()): number {
  const parsed = Date.parse(String(raw || ''));
  return Number.isNaN(parsed) ? fallback : parsed;
}

export function pretty(value: unknown): string {
  return JSON.stringify(value ?? {}, null, 2);
}

export function toInt(value: string): number | undefined {
  const n = Number(value);
  return Number.isFinite(n) && value.trim() ? Math.trunc(n) : undefined;
}

export function toFloat(value: string): number | undefined {
  const n = Number(value);
  return Number.isFinite(n) && value.trim() ? n : undefined;
}

export function cleanObject<T extends AnyRecord>(input: T): T {
  for (const key of Object.keys(input)) {
    if (input[key] === undefined || input[key] === null || input[key] === '') {
      delete input[key];
    }
  }
  return input;
}
