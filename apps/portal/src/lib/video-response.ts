// Strict reader of the video delivery the portal receives from GET /api/v1/portal/video/delivery.
export type VideoItem = { render_id: string; title: string; ordinal: number; duration_ticks: number; width: number; height: number; bytes: number };
export type VideoDelivery = { name: string; items: VideoItem[] };

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const TICKS_PER_SECOND = 705600000;
const bad = (): never => { throw new Error("Invalid portal response"); };
const object = (v: unknown): Record<string, unknown> => (v && typeof v === "object" && !Array.isArray(v) ? v as Record<string, unknown> : bad());
const text = (v: unknown, max: number): string => (typeof v === "string" && v.length >= 1 && v.length <= max ? v : bad());
const integer = (v: unknown, min: number, max = Number.MAX_SAFE_INTEGER): number => (Number.isSafeInteger(v) && (v as number) >= min && (v as number) <= max ? v as number : bad());

export function readVideoDelivery(v: unknown): VideoDelivery {
  const o = object(v);
  if (!Array.isArray(o.items) || o.items.length > 1000) return bad();
  const seen = new Set<string>();
  const items = o.items.map((raw): VideoItem => {
    const p = object(raw);
    const render_id = text(p.render_id, 36);
    if (!UUID.test(render_id) || seen.has(render_id)) return bad();
    seen.add(render_id);
    return { render_id, title: text(p.title, 200), ordinal: integer(p.ordinal, 0, 999), duration_ticks: integer(p.duration_ticks, 1), width: integer(p.width, 1, 16384), height: integer(p.height, 1, 16384), bytes: integer(p.bytes, 1) };
  });
  return { name: text(o.name, 120), items: items.sort((a, b) => a.ordinal - b.ordinal) };
}

export function durationLabel(ticks: number): string {
  const total = Math.round(ticks / TICKS_PER_SECOND);
  const h = Math.floor(total / 3600), m = Math.floor((total % 3600) / 60), s = total % 60;
  const pair = (n: number) => String(n).padStart(2, "0");
  return h ? `${h}:${pair(m)}:${pair(s)}` : `${m}:${pair(s)}`;
}
