export type DownloadLink = { url: string; expires_at: string; filename: string; bytes: number; sha256: string };
export function validateDownloadIntent(value: unknown, now = Date.now()): string {
  const v = value as { intent_token?: unknown; expires_at?: unknown } | null;
  const expires = typeof v?.expires_at === "string" ? Date.parse(v.expires_at) : NaN;
  if (typeof v?.intent_token !== "string" || !/^[A-Za-z0-9_-]{43}$/.test(v.intent_token) || !Number.isFinite(expires) || expires <= now || expires > now + 65000) throw new Error("Invalid download intent");
  return v.intent_token;
}
export function validateDownloadLink(value: unknown, now = Date.now()): DownloadLink {
  const v = value as DownloadLink | null;
  if (!v || typeof v.url !== "string" || typeof v.filename !== "string" || !v.filename.trim() || /[\r\n\0]/.test(v.filename) || !Number.isSafeInteger(v.bytes) || v.bytes <= 0 || typeof v.sha256 !== "string" || !/^[a-f0-9]{64}$/.test(v.sha256)) throw new Error("Invalid download link");
  const url = new URL(v.url);
  const local = ["localhost", "127.0.0.1", "[::1]"].includes(url.hostname);
  const expires = typeof v.expires_at === "string" ? Date.parse(v.expires_at) : NaN;
  if ((url.protocol !== "https:" && !(local && url.protocol === "http:")) || url.username || url.password || url.hash || url.searchParams.getAll("versionId").length !== 1 || !url.searchParams.get("versionId") || !Number.isFinite(expires) || expires <= now || expires > now + 65000) throw new Error("Invalid signed download URL");
  return { ...v };
}
