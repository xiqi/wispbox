// Minimal typed API client. Cookies carry the session; the CSRF token from
// login is echoed on every mutating request.

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

const CSRF_KEY = "wispbox.csrf";

export function setCsrf(token: string) {
  sessionStorage.setItem(CSRF_KEY, token);
}

export function clearCsrf() {
  sessionStorage.removeItem(CSRF_KEY);
}

function csrf(): string {
  return sessionStorage.getItem(CSRF_KEY) ?? "";
}

async function request(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  try {
    return await fetch(input, init);
  } catch {
    throw new ApiError(0, "Could not reach wispboxd. Check the server connection and try again.");
  }
}

async function parse(res: Response) {
  const text = await res.text();
  let body: any = null;
  try {
    body = text ? JSON.parse(text) : null;
  } catch {
    /* non-JSON error page */
  }
  if (!res.ok) {
    const msg = body?.error ?? `request failed (${res.status})`;
    throw new ApiError(res.status, msg);
  }
  return body;
}

export async function get<T = any>(url: string): Promise<T> {
  const res = await request(url, { credentials: "same-origin" });
  return parse(res);
}

export async function send<T = any>(
  method: string,
  url: string,
  body?: unknown,
): Promise<T> {
  const res = await request(url, {
    method,
    credentials: "same-origin",
    headers: {
      "Content-Type": "application/json",
      "X-wispbox-CSRF": csrf(),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  return parse(res);
}

export const post = <T = any>(url: string, body?: unknown) => send<T>("POST", url, body);
export const patch = <T = any>(url: string, body?: unknown) => send<T>("PATCH", url, body);
export const del = <T = any>(url: string) => send<T>("DELETE", url);

// Multipart POST (webmail compose with attachments).
export async function postForm<T = any>(url: string, form: FormData): Promise<T> {
  const res = await request(url, {
    method: "POST",
    credentials: "same-origin",
    headers: { "X-wispbox-CSRF": csrf() },
    body: form,
  });
  return parse(res);
}
