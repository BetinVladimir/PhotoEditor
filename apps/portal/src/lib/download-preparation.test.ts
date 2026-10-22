import { describe, expect, it, vi } from "vitest";
import { clearPreparation, hasPreparation, prepareDownload } from "./download-preparation";
function storage() {
 const entries = new Map<string,string>();
 return { getItem: (k:string) => entries.get(k) ?? null, setItem: (k:string,v:string) => {entries.set(k,v);}, removeItem: (k:string) => {entries.delete(k);} };
}
const preset = "12345678-1234-4123-8123-123456789abc";
describe("immutable download preparation", () => {
 it("replays the same job request after an ambiguous transport failure", async () => {
  const store = storage(); const share = "retry-start";
  const createSelection = vi.fn(async () => ({id:preset,item_count:2}));
  const startDownload = vi.fn().mockRejectedValueOnce(new Error("connection lost after commit")).mockResolvedValue({download_job_id:preset,status:"queued"});
  const api = {createSelection,startDownload}; const controller = new AbortController();
  await expect(prepareDownload(api,store,share,"csrf",{revision:"1",preset},controller.signal)).rejects.toThrow();
  expect(hasPreparation(store,share)).toBe(true);
  const firstKey = startDownload.mock.calls[0][4];
  const accepted = await prepareDownload(api,store,share,"csrf",{revision:"2",preset},controller.signal);
  expect(accepted.download_job_id).toBe(preset);
  expect(createSelection).toHaveBeenCalledTimes(1);
  expect(startDownload.mock.calls[1][4]).toBe(firstKey);
  clearPreparation(store,share);
  expect(hasPreparation(store,share)).toBe(false);
 });
 it("reuses selection key when its response was lost", async () => {
  const store = storage(); const share = "retry-selection";
  const createSelection = vi.fn().mockRejectedValueOnce(new Error("lost response")).mockResolvedValue({id:preset,item_count:2});
  const startDownload = vi.fn(async () => ({download_job_id:preset,status:"queued"}));
  const api = {createSelection,startDownload}; const controller = new AbortController();
  await expect(prepareDownload(api,store,share,"csrf",{revision:"1",preset},controller.signal)).rejects.toThrow();
  await prepareDownload(api,store,share,"csrf",{revision:"2",preset},controller.signal);
  expect(createSelection.mock.calls[1][4]).toBe(createSelection.mock.calls[0][4]);
  expect(createSelection.mock.calls[1][1]).toBe("1");
  clearPreparation(store,share);
 });
 it("does not start a job from an invalid selection receipt", async () => {
  const store = storage(); const share = "bad-selection";
  const createSelection = vi.fn(async () => ({id:preset,item_count:6000}));
  const startDownload = vi.fn(async () => ({download_job_id:preset,status:"queued"}));
  await expect(prepareDownload({createSelection,startDownload},store,share,"csrf",{revision:"1",preset},new AbortController().signal)).rejects.toThrow("Invalid selection receipt");
  expect(startDownload).not.toHaveBeenCalled(); clearPreparation(store,share);
 });
 it("does not expose a malformed accepted job receipt", async () => {
  const store = storage(); const share = "bad-job";
  const createSelection = vi.fn(async () => ({id:preset,item_count:2}));
  const startDownload = vi.fn(async () => ({download_job_id:"not-a-job",status:"ready"}));
  await expect(prepareDownload({createSelection,startDownload},store,share,"csrf",{revision:"1",preset},new AbortController().signal)).rejects.toThrow("Invalid download job receipt");
  clearPreparation(store,share);
 });

});
