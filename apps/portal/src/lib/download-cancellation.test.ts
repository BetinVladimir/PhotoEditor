import { afterEach, describe, expect, it, vi } from "vitest";
import { createSelection, startDownload, getDownload, cancelDownload, downloadItem, redeemDownload } from "./api";
afterEach(() => vi.unstubAllGlobals());
describe("portal download cancellation", () => {
  for (const kind of ["item", "volume"]) it(`never redeems ${kind} after late intent cancellation`, async () => {
    const controller = new AbortController();
    const fetchMock = vi.fn(async () => ({ ok: true, status:201, json: async () => {
      controller.abort();
      return { intent_token: "a".repeat(43), expires_at: new Date(Date.now() + 60000).toISOString() };
    } }));
    vi.stubGlobal("fetch", fetchMock);
    const result = kind === "item" ? downloadItem("csrf", "item", "preset", controller.signal) : redeemDownload("csrf", "job", 1, controller.signal);
    await expect(result).rejects.toMatchObject({ name: "AbortError" });
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
  it("makes no request for an already cancelled attempt", async () => {
    const controller = new AbortController(); controller.abort();
    const fetchMock = vi.fn(); vi.stubGlobal("fetch", fetchMock);
    await expect(redeemDownload("csrf", "job", 1, controller.signal)).rejects.toMatchObject({ name: "AbortError" });
    expect(fetchMock).not.toHaveBeenCalled();
  });
  it("cancels selection, job creation and polling before transport", async () => {
    const controller = new AbortController(); controller.abort();
    const fetchMock = vi.fn(); vi.stubGlobal("fetch", fetchMock);
    for (const request of [
      () => createSelection("csrf", "revision", ["item"], controller.signal),
      () => startDownload("csrf", "snapshot", "preset", controller.signal),
      () => getDownload("job", controller.signal),
      () => cancelDownload("csrf", "job", controller.signal),
    ]) await expect(request()).rejects.toMatchObject({ name: "AbortError" });
    expect(fetchMock).not.toHaveBeenCalled();
  });

});
