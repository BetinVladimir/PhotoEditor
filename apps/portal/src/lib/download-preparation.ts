export type Preparation = {
  revision: string; preset: string; items?: string[]; csrf?:string;
  selectionKey: string; startKey: string; ticket?:{download_job_id:string;status:string}; snapshot?: { id: string; item_count: number };
};
type StoragePort = Pick<Storage, "getItem" | "setItem" | "removeItem">;
type API = {
  createSelection(csrf: string, revision: string, items?: string[], signal?: AbortSignal, key?: string): Promise<{ id: string; item_count: number }>;
  startDownload(csrf: string, snapshot: string, preset: string, signal?: AbortSignal, key?: string): Promise<{ download_job_id: string; status: string }>;
};
const pending = new Map<string, Preparation>();
const keyFor = (share: string) => `photo:portal:${share}:download-preparation`;
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
export function clearPreparation(storage: StoragePort, share: string): void {
  storage.removeItem(keyFor(share));
  pending.delete(keyFor(share));
}
function read(storage: StoragePort, share: string): Preparation | undefined {
  const key = keyFor(share);
  if (pending.has(key)) return pending.get(key);
  try {
    const raw = storage.getItem(key); if (!raw) return;
    if (raw.length > 524288) throw new Error("Oversized preparation");
    const v = JSON.parse(raw) as Preparation;
    if (!v || Object.keys(v).some(k => !["revision", "preset", "items", "selectionKey", "startKey", "snapshot", "csrf", "ticket"].includes(k)) || (v.csrf!==undefined&&(typeof v.csrf!=="string"||!v.csrf||v.csrf.length>4096)) || typeof v.revision !== "string" || !/^[1-9][0-9]*$/.test(v.revision) || !Number.isSafeInteger(Number(v.revision)) || typeof v.preset !== "string" || !uuid.test(v.preset) || !uuid.test(v.selectionKey) || !uuid.test(v.startKey) || (v.items !== undefined && (!Array.isArray(v.items) || v.items.length < 1 || v.items.length > 5000 || v.items.some(i => typeof i !== "string" || !uuid.test(i)) || new Set(v.items).size !== v.items.length)) || (v.ticket!==undefined&&(!v.snapshot||!v.ticket||Object.keys(v.ticket).sort().join(",")!=="download_job_id,status"||!uuid.test(v.ticket.download_job_id)||!["queued","preparing","building","uploading","cancelling","cancelled","ready","failed","expired"].includes(v.ticket.status))) || (v.snapshot !== undefined && (!v.snapshot || Object.keys(v.snapshot).sort().join(",")!=="id,item_count" || !uuid.test(v.snapshot.id) || !Number.isSafeInteger(v.snapshot.item_count) || v.snapshot.item_count < 1 || v.snapshot.item_count > 5000))) throw new Error("Invalid preparation");
    pending.set(key, v); return v;
  } catch { throw new Error("PREPARATION_RECOVERY_UNAVAILABLE"); }
}
export function hasPreparation(storage: StoragePort, share: string): boolean { try{return read(storage, share)!==undefined;}catch{return true;} }
function save(storage: StoragePort, share: string, value: Preparation): void {
  pending.set(keyFor(share), value);
  storage.setItem(keyFor(share), JSON.stringify(value));
}
// Retry always reuses the immutable pending selection and both idempotency keys.
export async function prepareDownload(api: API, storage: StoragePort, share: string, csrf: string, request: { revision: string; preset: string; items?: string[] }, signal: AbortSignal) {
  signal.throwIfAborted();
  const value: Preparation = read(storage, share) ?? { ...request, csrf, items: request.items?.slice(), selectionKey: crypto.randomUUID(), startKey: crypto.randomUUID() };
  if(value.csrf!==undefined&&value.csrf!==csrf)throw new Error("PREPARATION_GUEST_CHANGED");
  if(value.csrf===undefined)value.csrf=csrf;
  save(storage, share, value);
  if(value.ticket&&value.snapshot)return {...value.ticket,item_count:value.snapshot.item_count};
  if (!value.snapshot) {
    const snapshot = await api.createSelection(csrf, value.revision, value.items, signal, value.selectionKey);
    signal.throwIfAborted();
    if (!snapshot || typeof snapshot.id !== "string" || !uuid.test(snapshot.id) || !Number.isSafeInteger(snapshot.item_count) || snapshot.item_count < 1 || snapshot.item_count > 5000 || (value.items && snapshot.item_count !== value.items.length)) throw new Error("Invalid selection receipt");
    value.snapshot = { id: snapshot.id, item_count: snapshot.item_count };
    save(storage, share, value);
  }
  const result = await api.startDownload(csrf, value.snapshot.id, value.preset, signal, value.startKey);
  signal.throwIfAborted();
  if (!result || typeof result.download_job_id !== "string" || !uuid.test(result.download_job_id) || !["queued", "preparing", "building", "uploading", "cancelling", "cancelled", "ready", "failed", "expired"].includes(result.status)) throw new Error("Invalid download job receipt");
  value.ticket={download_job_id:result.download_job_id,status:result.status};
  save(storage,share,value);
  return { download_job_id: result.download_job_id, status: result.status, item_count: value.snapshot.item_count };
}

// Clearing is conditional on both frozen keys; an old response cannot erase a
// replacement preparation. Guest CSRF is an opaque scope pin, not a session ID.
export async function abandonPreparation(api:Pick<typeof import('./api'),'abandonDownloadPreparation'>,storage:StoragePort,share:string,csrf:string,signal:AbortSignal){
  signal.throwIfAborted();const value=read(storage,share);if(!value)throw new Error("PREPARATION_REQUIRED");
  if(value.csrf!==undefined&&value.csrf!==csrf)throw new Error("PREPARATION_GUEST_CHANGED");
  const selection=value.items?{collection_revision:value.revision,scope:"selected",item_ids:[...value.items]}:{collection_revision:value.revision,scope:"all"};
  const result=await api.abandonDownloadPreparation(csrf,{selection_key:value.selectionKey,start_key:value.startKey,selection,preset_id:value.preset,snapshot_id:value.snapshot?.id??null},signal);
  signal.throwIfAborted();
  const current=read(storage,share);
  if(!current||current.selectionKey!==value.selectionKey||current.startKey!==value.startKey)throw new Error("PREPARATION_REPLACED");
  return {result,item_count:value.snapshot?.item_count??value.items?.length??0,clear:()=>{const latest=read(storage,share);if(latest?.selectionKey===value.selectionKey&&latest.startKey===value.startKey)clearPreparation(storage,share);}};
}

export const preparationScope=(share:string,csrf:string)=>`${share}:guest:${encodeURIComponent(csrf)}`;
// Move a pre-scope journal only while the browser still holds its original
// opaque CSRF token, before exchanging a new guest. Never discard malformed data.
export function scopeLegacyPreparation(storage:StoragePort,share:string,csrf:string):void{
  if(!csrf)return;
  const old=read(storage,share);if(!old)return;
  if(old.csrf!==undefined&&old.csrf!==csrf)throw new Error("PREPARATION_GUEST_CHANGED");
  const scoped=preparationScope(share,csrf),existing=read(storage,scoped);
  if(existing&&(existing.selectionKey!==old.selectionKey||existing.startKey!==old.startKey))throw new Error("PREPARATION_REPLACED");
  save(storage,scoped,{...old,csrf});
  // Durable destination exists before removing the old alias.
  storage.removeItem(keyFor(share));pending.delete(keyFor(share));
}
