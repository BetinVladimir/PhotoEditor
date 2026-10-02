import {readEventStream,type StreamEvent} from "./sse";
export type Principal = { kind: "owner"; accessToken: string } | { kind: "guest"; shareID: string; sessionEpoch: number };
export type RequestOptions = { method?: string; body?: unknown; headers?: HeadersInit; idempotencyKey?: string; signal?: AbortSignal };
export type APIErrorBody = { code?: string; message?: string; error?: { code?: string; message?: string } };
export { canonicalJSON } from "./canonical";
import { canonicalJSON } from "./canonical";

export class APIError extends Error {
  constructor(readonly status: number, readonly code: string, message?: string, readonly requestID?: string) { super(message || code); }
}

export interface AuthStore {
  principal(): Principal | null;
  refresh(signal?: AbortSignal): Promise<string>;
  clear(): Promise<void> | void;
  csrfToken?(): string | undefined;
  clientHeaders?(): HeadersInit;
}

const safeRetry = (method: string, key?: string) => method === "GET" || method === "HEAD" || (!!key && method === "POST");
const pause = (ms: number, signal?: AbortSignal) => new Promise<void>((resolve, reject) => {
  const id = setTimeout(resolve, ms);
  signal?.addEventListener("abort", () => { clearTimeout(id); reject(signal.reason || new DOMException("Aborted", "AbortError")); }, { once: true });
});

export class ApiClient {
  private refreshFlight?: Promise<string>;
  private csrfFlight?: Promise<string>;
  constructor(readonly origin: string, private auth: AuthStore, private transport: typeof fetch = fetch) {}

  private async csrf(signal?: AbortSignal): Promise<string> {
    const saved = this.auth.csrfToken?.();
    if (saved) return saved;
    if (!this.csrfFlight) this.csrfFlight = this.raw<{ csrf_token: string }>("/api/v1/auth/csrf", { method: "GET", signal }, false).then(x => x.csrf_token).finally(() => { this.csrfFlight = undefined; });
    return this.csrfFlight;
  }

  async request<T>(path: string, options: RequestOptions = {}): Promise<T> {
    const method = (options.method || "GET").toUpperCase();
    let refreshed = false;
    for (let networkAttempt = 0; ; networkAttempt++) {
      try {
        return await this.raw<T>(path, { ...options, method }, true);
      } catch (error) {
        if (error instanceof APIError && error.status === 401 && !refreshed && this.auth.principal()?.kind === "owner") {
          refreshed = true;
          if (!this.refreshFlight) this.refreshFlight = this.auth.refresh(options.signal).finally(() => { this.refreshFlight = undefined; });
          try { await this.refreshFlight; } catch (refreshError) { await this.auth.clear(); throw refreshError; }
          continue;
        }
        if (!(error instanceof APIError) && networkAttempt < 2 && safeRetry(method, options.idempotencyKey)) {
          await pause(100 * 2 ** networkAttempt, options.signal);
          continue;
        }
        throw error;
      }
    }
  }

  async eventStream(path:string,cursor:string|undefined,onEvent:(event:StreamEvent)=>void,signal:AbortSignal):Promise<void>{
    if(!path.startsWith("/api/v1/"))throw new Error("API path must be versioned");
    for(let attempt=0;attempt<2;attempt++){
      signal.throwIfAborted();
      const principal=this.auth.principal();
      if(principal?.kind!=="owner")throw new APIError(401,"AUTH_REQUIRED");
      const headers=new Headers(this.auth.clientHeaders?.());
      headers.set("Accept","text/event-stream");headers.set("Authorization",`Bearer ${principal.accessToken}`);
      if(cursor)headers.set("Last-Event-ID",cursor);
      const response=await this.transport(`${this.origin.replace(/\/$/,"")}${path}`,{headers,signal,credentials:"include",cache:"no-store"});
      if(response.status===401&&attempt===0){
        await response.body?.cancel();
        if(!this.refreshFlight)this.refreshFlight=this.auth.refresh(signal).finally(()=>{this.refreshFlight=undefined});
        await this.refreshFlight;continue;
      }
      if(!response.ok){await response.body?.cancel();throw new APIError(response.status,`HTTP_${response.status}`)}
      if(!response.headers.get("Content-Type")?.startsWith("text/event-stream")||!response.body){await response.body?.cancel();throw new Error("SSE unavailable")}
      await readEventStream(response.body,onEvent,signal);return;
    }
  }

  private async raw<T>(path: string, options: RequestOptions, attachAuth: boolean): Promise<T> {
    if (!path.startsWith("/api/v1/")) throw new Error("API path must be versioned");
    const method = (options.method || "GET").toUpperCase();
    const headers = new Headers(options.headers);
    const clientHeaders = this.auth.clientHeaders?.();
    if (clientHeaders) new Headers(clientHeaders).forEach((value, key) => headers.set(key, value));
    headers.set("Accept", "application/json");
    const principal = attachAuth ? this.auth.principal() : null;
    if (principal?.kind === "owner") headers.set("Authorization", `Bearer ${principal.accessToken}`);
    if (options.idempotencyKey) headers.set("Idempotency-Key", options.idempotencyKey);
    const nativeBootstrap = headers.get("X-Photo-Client") === "native" && path.startsWith("/api/v1/auth/otp/");
    if (!['GET','HEAD','OPTIONS'].includes(method) && !nativeBootstrap) headers.set("X-CSRF-Token", await this.csrf(options.signal));
    let body: BodyInit | undefined;
    if (options.body !== undefined) { headers.set("Content-Type", "application/json"); body = canonicalJSON(options.body); }
    const response = await this.transport(`${this.origin.replace(/\/$/, "")}${path}`, { method, headers, body, signal: options.signal, credentials: "include", cache: "no-store" });
    if (response.ok) return response.status === 204 ? undefined as T : response.json() as Promise<T>;
    let decoded: APIErrorBody = {};
    try { decoded = await response.json() as APIErrorBody; } catch { /* generic public failure */ }
    throw new APIError(response.status, decoded.error?.code || decoded.code || `HTTP_${response.status}`, decoded.error?.message || decoded.message, response.headers.get("X-Request-ID") || undefined);
  }
}

export const QueryKeys = {
  tenant: (tenant: string, principal: string, resource: string, filters: unknown = null) => ["tenant", tenant, principal, resource, filters] as const,
  guest: (shareID: string, sessionEpoch: number, resource: string, filters: unknown = null) => ["guest", shareID, sessionEpoch, resource, filters] as const,
};

export type Selection = { mode: "explicit"; itemIDs: string[]; collectionRevision: string } | { mode: "snapshot"; snapshotID: string; itemCount: number; collectionRevision: string };
export class SelectionStore {
  value: Selection | null = null;
  explicit(ids: string[], revision: string) { this.value = { mode: "explicit", itemIDs: [...new Set(ids)], collectionRevision: revision }; }
  snapshot(id: string, count: number, revision: string) { if (count < 0) throw new Error("invalid selection count"); this.value = { mode: "snapshot", snapshotID: id, itemCount: count, collectionRevision: revision }; }
  changeScope() { this.value = null; }
}

export type EditorState<P, M> = { versionId: string; sourceSha: string; draftRevision: number; params: P; maskRefs: M[]; historyIndex: number };
export class EditorStore<P, M> {
  private history: Array<{ params: P; maskRefs: M[] }>;
  state: EditorState<P, M>;
  constructor(versionId: string, sourceSha: string, params: P, maskRefs: M[] = []) { this.history = [{ params, maskRefs: [...maskRefs] }]; this.state = { versionId, sourceSha, draftRevision: 0, params, maskRefs: [...maskRefs], historyIndex: 0 }; }
  apply(params: P, maskRefs = this.state.maskRefs) { this.history.splice(this.state.historyIndex + 1); this.history.push({ params, maskRefs: [...maskRefs] }); this.restore(this.history.length - 1); }
  undo() { if (this.state.historyIndex > 0) this.restore(this.state.historyIndex - 1); }
  redo() { if (this.state.historyIndex + 1 < this.history.length) this.restore(this.state.historyIndex + 1); }
  accepts(response: { draftRevision: number; sourceSha: string }) { return response.draftRevision === this.state.draftRevision && response.sourceSha === this.state.sourceSha; }
  private restore(index: number) { const h = this.history[index]; this.state = { ...this.state, params: h.params, maskRefs: [...h.maskRefs], historyIndex: index, draftRevision: this.state.draftRevision + 1 }; }
}

export type EventEnvelope = { cursor: string; type: string; payload: unknown };
export class EventPoller {
  private controller?: AbortController;
  constructor(private load: (cursor: string | undefined, signal: AbortSignal) => Promise<{ items: EventEnvelope[]; next_cursor?: string }>, private visible = () => typeof document === "undefined" || document.visibilityState === "visible") {}
  stop() { this.controller?.abort(); this.controller = undefined; }
  async start(cursor: string | undefined, onEvent: (event: EventEnvelope) => void, onExpired: () => Promise<void> | void) {
    this.stop(); const controller = new AbortController(); this.controller = controller; let delay = 2000;
    while (!controller.signal.aborted) {
      try { const page = await this.load(cursor, controller.signal); for (const event of page.items) { onEvent(event); cursor = event.cursor; } cursor = page.next_cursor || cursor; delay = this.visible() ? (page.items.length ? 2000 : 5000) : 30000; }
      catch (e) { if (controller.signal.aborted) return; if (e instanceof APIError && e.code === "EVENT_CURSOR_EXPIRED") { await onExpired(); cursor = undefined; } delay = Math.min(30000, Math.max(2000, delay * 2)); }
      await pause(delay + Math.floor(Math.random() * Math.min(500, delay / 4)), controller.signal).catch(() => undefined);
    }
  }
}
export * from "./upload";
