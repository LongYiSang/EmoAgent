export function matchesQuery(query: string, ...values: unknown[]) {
  const needle = query.trim().toLowerCase();
  if (!needle) return true;
  return values.map(value => String(value || '').toLowerCase()).join(' ').includes(needle);
}
