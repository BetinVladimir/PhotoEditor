import { describe, expect, it } from "vitest";
import { validateDownloadIntent, validateDownloadLink } from "./download-link";
const now = Date.parse("2026-10-17T12:00:00Z");
const link = { url: "https://storage.test/file?versionId=immutable", filename: "photos.zip", bytes: 100, sha256: "a".repeat(64), expires_at: new Date(now + 60000).toISOString() };
describe("portal download response boundaries", () => {
  it("accepts a current immutable signed object", () => expect(validateDownloadLink(link, now)).toEqual(link));
  it("rejects redirect credentials, unversioned and ambiguous objects", () => {
    for (const url of ["javascript:alert(1)", "https://user:pass@storage.test/file?versionId=v", "https://storage.test/file", "https://storage.test/file?versionId=a&versionId=b", "https://storage.test/file?versionId=v#secret"]) expect(() => validateDownloadLink({ ...link, url }, now)).toThrow();
  });
  it("rejects expired and overlong links", () => {
    for (const offset of [0, -1, 66000]) expect(() => validateDownloadLink({ ...link, expires_at: new Date(now + offset).toISOString() }, now)).toThrow();
  });
  it("validates bearer lifetime before redemption", () => {
    expect(validateDownloadIntent({ intent_token: "a".repeat(43), expires_at: link.expires_at }, now)).toHaveLength(43);
    expect(() => validateDownloadIntent({ intent_token: "opaque", expires_at: link.expires_at }, now)).toThrow();
  });
});
