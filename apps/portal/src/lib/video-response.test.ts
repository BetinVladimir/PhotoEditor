import { describe, expect, it } from "vitest";
import { durationLabel, readVideoDelivery } from "./video-response";

const id = "11111111-1111-4111-8111-111111111111", other = "22222222-2222-4222-8222-222222222222";
const item = (patch: Record<string, unknown> = {}) => ({ render_id: id, title: "Full film", ordinal: 0, duration_ticks: 4233600000, width: 1280, height: 720, bytes: 1300000, ...patch });

describe("video delivery boundary", () => {
  it("reads a delivery and orders items by ordinal", () => {
    const out = readVideoDelivery({ name: "Wedding", items: [item({ render_id: other, ordinal: 1 }), item()] });
    expect(out.items.map(i => i.render_id)).toEqual([id, other]);
  });
  it("refuses malformed, duplicate and oversized values", () => {
    for (const bad of [null, [], { name: "", items: [] }, { name: "x", items: "no" }, { name: "x", items: [item({ render_id: "nope" })] },
      { name: "x", items: [item(), item()] }, { name: "x", items: [item({ width: 0 })] }, { name: "x", items: [item({ bytes: 1.5 })] }, { name: "x", items: [item({ title: "t".repeat(201) })] }]) {
      expect(() => readVideoDelivery(bad)).toThrow("Invalid portal response");
    }
  });
  it("formats durations", () => {
    expect(durationLabel(4233600000)).toBe("0:06");
    expect(durationLabel(705600000 * 3725)).toBe("1:02:05");
  });
});
