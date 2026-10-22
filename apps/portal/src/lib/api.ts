import {readSession,readCollection,readItems,readMedia,readSelection,readDownloadAccepted,readUpload,readParts,readUploadAccepted,readBootstrap,readError} from './response';
import { validateDownloadStatus } from "./download-state";
import { validateDownloadIntent, validateDownloadLink } from "./download-link";
import type { APIErrorBody, Collection, DownloadStatus, GuestItem, MediaLink, Page, PartURL, Upload } from "./types";

export class APIError extends Error {
  constructor(public status: number, public code: string, message?: string) { super(message || code); }
}

const base = () => (process.env.NEXT_PUBLIC_API_ORIGIN || "").replace(/\/$/, "");
async function decode<T>(response: Response): Promise<T> {
  if (response.ok) return response.json() as Promise<T>;
  let body: APIErrorBody = {};
  try { body = await response.json() as APIErrorBody; } catch { /* intentionally generic */ }
  const error=readError(body);
  throw new APIError(response.status,error.code || `HTTP_${response.status}`,error.message);
}
async function request<T>(path: string, init: RequestInit = {}, successStatuses: readonly number[] = [200]): Promise<T> {
  init.signal?.throwIfAborted();
  const response = await fetch(`${base()}${path}`, { ...init, credentials: "include", cache: "no-store", headers: { Accept: "application/json", ...init.headers } });
  init.signal?.throwIfAborted();
  if(response.ok&&!successStatuses.includes(response.status))throw new Error("Invalid portal response status");
  const result = await decode<T>(response);
  init.signal?.throwIfAborted();
  return result;
}
const json = (value: unknown) => JSON.stringify(value);
export const idempotencyKey = (): string => crypto.randomUUID();

export async function exchange(shareID: string, secret: string, locale: string, signal?: AbortSignal) {
  const bootstrap = readBootstrap(await request<unknown>("/api/v1/auth/csrf", { signal }));
  return readSession(await request<unknown>("/api/v1/portal/sessions", { signal, method: "POST", headers: { "Content-Type": "application/json", "X-CSRF-Token": bootstrap }, body: json({ share_id: shareID, secret, locale }) },[201]));
}
export const getCollection = async (signal?: AbortSignal) => readCollection(await request<unknown>("/api/v1/portal/collection", { signal }));
export const getItems = async (cursor?: string, sectionID?: string, signal?: AbortSignal, expectedRevision?:string) => {
  const query = new URLSearchParams({ limit: "100" });
  if (cursor) query.set("cursor", cursor);
  if (sectionID) query.set("section_id", sectionID);
  return readItems(await request<unknown>(`/api/v1/portal/items?${query}`, { signal }),cursor,sectionID,expectedRevision);
};
export const getMediaLinks = async (csrf: string, itemIDs: string[], variants: string[], signal?: AbortSignal) => {
  const ids=[...itemIDs], names=[...variants];
  return readMedia(await request<unknown>("/api/v1/portal/media-links", { method:"POST",signal,headers:{"Content-Type":"application/json","X-CSRF-Token":csrf},body:json({item_ids:ids,variants:names}) }),ids,names);
};
export const createSelection = async (csrf: string, revision: string, itemIDs?: string[], signal?: AbortSignal, key = idempotencyKey()) => {
  const ids=itemIDs?[...itemIDs]:undefined;
  return readSelection(await request<unknown>("/api/v1/portal/selection-snapshots",{method:"POST",signal,headers:{"Content-Type":"application/json","X-CSRF-Token":csrf,"Idempotency-Key":key},body:json(ids?{collection_revision:revision,scope:"selected",item_ids:ids}:{collection_revision:revision,scope:"all"})},[201]),ids?.length);
};
export const startDownload = async (csrf: string, snapshotID: string, presetID: string, signal?: AbortSignal, key = idempotencyKey()) => readDownloadAccepted(await request<unknown>("/api/v1/portal/download-jobs", { method:"POST",signal,headers:{"Content-Type":"application/json","X-CSRF-Token":csrf,"Idempotency-Key":key},body:json({selection_snapshot_id:snapshotID,preset_id:presetID}) },[202]));
export const getDownload = async (id: string, signal?: AbortSignal) => validateDownloadStatus(await request<unknown>(`/api/v1/portal/download-jobs/${id}`, { signal }), id);
export const cancelDownload = async (csrf: string, id: string, signal?: AbortSignal) => validateDownloadStatus(await request<unknown>(`/api/v1/portal/download-jobs/${id}/cancel`, { signal, method: "POST", headers: { "X-CSRF-Token": csrf } }), id);
export async function redeemDownload(csrf: string, id: string, volume: number, signal?: AbortSignal) {
  const intent = await request<{ intent_token: string }>(`/api/v1/portal/download-jobs/${id}/download-intent`, { method: "POST", signal, headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf, "Idempotency-Key": idempotencyKey() }, body: json({ volume }) },[201]);
  signal?.throwIfAborted();
  return validateDownloadLink(await request<unknown>("/api/v1/downloads/redeem", { method: "POST", signal, headers: { "Content-Type": "application/json" }, body: json({ token: validateDownloadIntent(intent) }) }));
}
export async function downloadItem(csrf: string, itemID: string, presetID: string, signal?: AbortSignal) {
  const intent = await request<{ intent_token: string }>(`/api/v1/portal/items/${itemID}/download-intent`, { method: "POST", signal, headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf, "Idempotency-Key": idempotencyKey() }, body: json({ preset_id: presetID }) },[201]);
  signal?.throwIfAborted();
  return validateDownloadLink(await request<unknown>("/api/v1/downloads/redeem", { method: "POST", signal, headers: { "Content-Type": "application/json" }, body: json({ token: validateDownloadIntent(intent) }) }));
}
export const beginUpload = async (csrf: string, file: File, signal?: AbortSignal, key=idempotencyKey()) => {
  const expected={filename:file.name,bytes:file.size};
  return readUpload(await request<unknown>("/api/v1/portal/uploads",{method:"POST",signal,headers:{"Content-Type":"application/json","X-CSRF-Token":csrf,"Idempotency-Key":key},body:json({...expected,format:(file.name.split(".").pop()||"").toLowerCase()})},[201]),undefined,expected);
};
export const signParts = async (csrf: string, uploadID: string, partNumbers: number[], signal?: AbortSignal) => {
  const parts=[...partNumbers];
  return readParts(await request<unknown>(`/api/v1/portal/uploads/${uploadID}/parts`,{method:"POST",signal,headers:{"Content-Type":"application/json","X-CSRF-Token":csrf},body:json({part_numbers:parts})}),uploadID,parts);
};
export const completeUpload = async (csrf: string, upload: Upload, parts: {part_no:number;etag:string}[], signal?: AbortSignal, key=idempotencyKey()) => readUploadAccepted(await request<unknown>(`/api/v1/portal/uploads/${upload.id}/complete`,{method:"POST",signal,headers:{"Content-Type":"application/json","X-CSRF-Token":csrf,"Idempotency-Key":key},body:json({parts,expected_size:upload.expected_size})},[202]));
export const abortUpload = async (csrf: string, uploadID: string, signal?: AbortSignal) => readUpload(await request<unknown>(`/api/v1/portal/uploads/${uploadID}/abort`,{method:"POST",signal,headers:{"X-CSRF-Token":csrf}}),uploadID);

export function secretFromFragment(hash: string): string | null {
  const raw = hash.replace(/^#/, "");
  if (!raw) return null;
  const params = new URLSearchParams(raw);
  const value = params.get("k") || params.get("secret") || (raw.includes("=") ? "" : raw);
  return value && value.length >= 32 && value.length <= 256 ? value : null;
}

export async function abandonDownloadPreparation(csrf:string, body:{selection_key:string;start_key:string;selection:{collection_revision:string;scope:string;item_ids?:string[]};preset_id:string;snapshot_id:string|null}, signal?:AbortSignal) {
  const frozen=JSON.parse(JSON.stringify(body)) as typeof body;
  const result=await request<unknown>("/api/v1/portal/download-preparations/abandon",{method:"POST",signal,headers:{"Content-Type":"application/json","X-CSRF-Token":csrf},body:json(frozen)});
  signal?.throwIfAborted();
  if(!result||typeof result!=="object"||Array.isArray(result))throw new Error("Invalid preparation resolution");
  const v=result as Record<string,unknown>;
  if(v.selection_key!==frozen.selection_key||v.start_key!==frozen.start_key||v.preset_id!==frozen.preset_id)throw new Error("Foreign preparation resolution");
  if(v.status==="accepted"&&Object.keys(v).sort().join(",")==="preset_id,selection_key,start_key,status,ticket"){
    if(!v.ticket||typeof v.ticket!=="object"||Array.isArray(v.ticket)||Object.keys(v.ticket).sort().join(",")!=="download_job_id,status")throw new Error("Invalid preparation ticket");
    const ticket=readDownloadAccepted(v.ticket);if(ticket.status!=="queued")throw new Error("Invalid preparation ticket");
    return {status:"accepted" as const,ticket};
  }
  if(v.status==="abandoned"&&typeof v.collection_id==="string"&&/^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(v.collection_id)&&Object.keys(v).sort().join(",")==="collection_id,preset_id,selection_key,start_key,status")return {status:"abandoned" as const};
  throw new Error("Invalid preparation resolution");
}
