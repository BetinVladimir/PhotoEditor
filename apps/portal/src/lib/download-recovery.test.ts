import { describe, expect, it } from "vitest";
import { readDownloadJob, rememberDownloadJob } from "./download-recovery";
function storage() {
  const data = new Map<string, string>();
  return { getItem: (key: string) => data.get(key) ?? null, setItem: (key: string, value: string) => { data.set(key, value); }, removeItem: (key: string) => { data.delete(key); }, data };
}
describe("download job recovery", () => {
  const id = "12345678-1234-4123-8123-123456789abc";
  it("isolates shares and persists only job identity", () => {
    const store = storage(); rememberDownloadJob(store, "a", id);
    expect(readDownloadJob(store, "a")).toBe(id);
    expect(readDownloadJob(store, "b")).toBeNull();
    expect([...store.data.values()]).toEqual([id]);
    rememberDownloadJob(store, "a", null);
    expect(readDownloadJob(store, "a")).toBeNull();
  });
  it("discards tampered identities", () => {
    const store = storage(); store.setItem("photo:portal:a:download-job", "https://evil.test");
    expect(readDownloadJob(store, "a")).toBeNull(); expect(store.data.size).toBe(0);
  });
});
