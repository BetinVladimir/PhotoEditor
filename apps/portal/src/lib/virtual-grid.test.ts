import { describe, expect, it } from "vitest";
import { virtualWindow } from "./virtual-grid";

describe("virtualWindow", () => {
  it("keeps a 5000 item collection to a bounded rendered window", () => {
    const first = virtualWindow(5000, 5, 240, 0, 900);
    const middle = virtualWindow(5000, 5, 240, 100000, 900);
    expect(first.end - first.start).toBeLessThanOrEqual(35);
    expect(middle.end - middle.start).toBeLessThanOrEqual(60);
    expect(middle.start).toBeGreaterThan(1000);
    expect(middle.totalHeight).toBe(240000);
  });
  it("clamps the last partial row", () => expect(virtualWindow(11, 3, 100, 1000, 500)).toMatchObject({ end: 11, totalHeight: 400 }));
  it("rejects invalid geometry", () => expect(() => virtualWindow(1, 0, 100, 0, 1)).toThrow());
});
