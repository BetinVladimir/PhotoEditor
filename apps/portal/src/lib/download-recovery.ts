const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
const key = (shareID: string) => `photo:portal:${shareID}:download-job`;
export function readDownloadJob(storage: Pick<Storage, "getItem" | "removeItem">, shareID: string): string | null {
  try {
    const value = storage.getItem(key(shareID));
    if (value === null) return null;
    if (uuid.test(value)) return value;
    storage.removeItem(key(shareID));
  } catch { /* Storage may be unavailable; downloads remain usable. */ }
  return null;
}
export function rememberDownloadJob(storage: Pick<Storage, "setItem" | "removeItem">, shareID: string, id: string | null): void {
  try {
    if (id !== null && uuid.test(id)) storage.setItem(key(shareID), id);
    else storage.removeItem(key(shareID));
  } catch { /* Recovery is optional when browser storage is unavailable. */ }
}
