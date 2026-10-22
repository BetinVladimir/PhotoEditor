import type { MediaLink } from "./types";

export class MediaCache {
  private values = new Map<string, MediaLink>();
  constructor(private readonly maxEntries = 512) { if (!Number.isInteger(maxEntries) || maxEntries < 1) throw new Error("invalid media cache limit"); }
  get(itemID: string, variant: string, now = Date.now()) {
    const key = `${itemID}:${variant}`;
    const value = this.values.get(key);
    if (!value || Date.parse(value.expires_at) - now < 30_000) { this.values.delete(key); return undefined; }
    this.values.delete(key); this.values.set(key, value); return value;
  }
  put(items: MediaLink[]) { for (const item of items) { const key=`${item.item_id}:${item.variant}`;this.values.delete(key);this.values.set(key,item);while(this.values.size>this.maxEntries)this.values.delete(this.values.keys().next().value!); } }
  clear() { this.values.clear(); }
}
