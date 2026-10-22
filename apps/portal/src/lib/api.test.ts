import { describe, expect, it } from "vitest";
import { secretFromFragment } from "./api";
import { MediaCache } from "./media-cache";

describe("secretFromFragment", () => {
  const secret = "a".repeat(32);
  it("accepts named and raw fragments", () => { expect(secretFromFragment(`#k=${secret}`)).toBe(secret); expect(secretFromFragment(`#${secret}`)).toBe(secret); });
  it("rejects absent, short, and oversized secrets", () => { expect(secretFromFragment("")).toBeNull(); expect(secretFromFragment("#k=short")).toBeNull(); expect(secretFromFragment(`#${"a".repeat(257)}`)).toBeNull(); });
});

describe("MediaCache", () => {
  it("keeps valid links and evicts links close to expiry", () => {
    const cache = new MediaCache();
    cache.put([{ item_id: "one", variant: "thumb640", url: "https://object.test/a", expires_at: "2026-01-01T00:02:00Z", width: 1, height: 1, media_type: "image/jpeg" }]);
    expect(cache.get("one", "thumb640", Date.parse("2026-01-01T00:00:00Z"))?.url).toContain("object.test");
    expect(cache.get("one", "thumb640", Date.parse("2026-01-01T00:01:40Z"))).toBeUndefined();
  });
  it("evicts least recently used links at its hard memory bound", () => {
    const cache = new MediaCache(2); const expires_at="2099-01-01T00:00:00Z";
    const link=(item_id:string): Parameters<typeof cache.put>[0][number]=>({item_id,variant:"thumb640",url:`https://object.test/${item_id}`,expires_at,width:1,height:1,media_type:"image/jpeg"});
    cache.put([link("one"),link("two")]);cache.get("one","thumb640");cache.put([link("three")]);
    expect(cache.get("two","thumb640")).toBeUndefined();expect(cache.get("one","thumb640")).toBeDefined();
  });
});
