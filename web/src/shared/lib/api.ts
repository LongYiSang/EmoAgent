export type AnyRecord = Record<string, unknown>;

export class ApiError extends Error {
  status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
  }
}

type RequestOptions = Omit<RequestInit, 'body'> & {
  body?: BodyInit | AnyRecord | null;
};

export async function requestJSON<T>(url: string, options: RequestOptions = {}): Promise<T> {
  const headers = new Headers(options.headers);
  headers.set('Accept', 'application/json');

  let body = options.body ?? null;
  if (body && typeof body === 'object' && !(body instanceof FormData) && !(body instanceof Blob) && !(body instanceof URLSearchParams)) {
    headers.set('Content-Type', 'application/json');
    body = JSON.stringify(body);
  }

  const response = await fetch(url, {
    ...options,
    body,
    headers,
    credentials: 'same-origin',
  });
  const raw = await response.text();
  const payload = raw ? parseJSON(raw) : {};
  if (!response.ok) {
    const message = typeof payload.error === 'string' ? payload.error : response.statusText || 'Request failed';
    throw new ApiError(message, response.status);
  }
  return payload as T;
}

function parseJSON(raw: string): AnyRecord {
  try {
    const parsed = JSON.parse(raw);
    return parsed && typeof parsed === 'object' ? parsed as AnyRecord : {};
  } catch {
    return {};
  }
}
