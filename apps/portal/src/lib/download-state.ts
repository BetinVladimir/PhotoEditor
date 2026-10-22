// Public download_jobs state; the underlying run uses a separate state machine.
export function downloadReady(status: string): boolean { return status === "ready"; }
export function downloadTerminal(status: string): boolean {
  return ["ready", "failed", "cancelled", "expired"].includes(status);
}

import type { DownloadStatus } from "./types";
export function validateDownloadStatus(value: unknown, expectedID: string): DownloadStatus {
  const v = value as DownloadStatus | null;
  if (!v || v.id !== expectedID || typeof v.status !== "string" || !["queued","preparing","building","uploading","ready","expired","failed","cancelling","cancelled"].includes(v.status) || !Number.isSafeInteger(v.files_done) || !Number.isSafeInteger(v.files_total) || v.files_done < 0 || v.files_total < v.files_done || !Number.isSafeInteger(v.bytes_written) || v.bytes_written < 0 || !Array.isArray(v.volumes) || v.volumes.length > 5000) throw new Error("Invalid archive status");
  const seen = new Set<number>();
  for (const volume of v.volumes) {
    if (!volume || !Number.isSafeInteger(volume.number) || volume.number < 1 || volume.number > 5000 || seen.has(volume.number) || typeof volume.status !== "string" || !volume.status || !Number.isSafeInteger(volume.bytes) || volume.bytes < 0 || !["single", "zip"].includes(volume.delivery_kind)) throw new Error("Invalid archive volume");
    seen.add(volume.number);
  }
  return { ...v, volumes: v.volumes.map(volume => ({ ...volume })) };
}

// A replayed Start receipt can already be terminal but carries no volume/failure inventory.
// Fetch its complete status before terminal polling can stop.
export function downloadNeedsPolling(state:string,receiptPending:boolean,visible:boolean){return visible&&(receiptPending||!downloadTerminal(state))}
