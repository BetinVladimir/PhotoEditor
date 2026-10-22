import { describe, expect, it } from "vitest";
import { downloadReady, downloadTerminal, validateDownloadStatus } from "./download-state";
describe("public archive job lifecycle", () => {
  it("offers download and stops polling at the server ready state", () => {
    expect(downloadReady("ready")).toBe(true);
    expect(downloadTerminal("ready")).toBe(true);
    expect(downloadReady("completed")).toBe(false);
  });
  it("keeps cancellation pending until the server finalizes it", () => {
    for (const state of ["queued", "preparing", "building", "uploading", "cancelling"]) {
      expect(downloadReady(state)).toBe(false); expect(downloadTerminal(state)).toBe(false);
    }
    for (const state of ["failed", "cancelled", "expired"]) {
      expect(downloadReady(state)).toBe(false); expect(downloadTerminal(state)).toBe(true);
    }
  });
});

it("rejects wrong-job and ambiguous archive volumes", () => {
  const value = { id: "job", status: "ready", files_done: 2, files_total: 2, bytes_written: 100, volumes: [{ number: 1, status: "ready", bytes: 100, delivery_kind: "zip" }] };
  expect(validateDownloadStatus(value, "job")).toEqual(value);
  expect(() => validateDownloadStatus(value, "another-job")).toThrow();
  expect(() => validateDownloadStatus({ ...value, volumes: [...value.volumes, ...value.volumes] }, "job")).toThrow();
  expect(() => validateDownloadStatus({ ...value, files_done: 3 }, "job")).toThrow();
  expect(() => validateDownloadStatus({ ...value, volumes: null }, "job")).toThrow();
});

it('refuses undeclared job states instead of polling forever on malformed success',()=>{
 expect(()=>validateDownloadStatus({id:'job',status:'succeeded',files_done:0,files_total:0,bytes_written:0,volumes:[]},'job')).toThrow();
});
