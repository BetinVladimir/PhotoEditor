import {describe,it,expect,vi} from "vitest";
import {sha256} from "@noble/hashes/sha2.js";
import {bytesToHex} from "@noble/hashes/utils.js";
import type {UploadSource} from "@photoeditor/api-client";
import {verifyUploadSource} from "./upload-source-integrity";
describe("upload source integrity",()=>{
 it("hashes exact bounded ranges including the tail",async()=>{
  const data=new Uint8Array(1024*1024+3).fill(17);
  const read=vi.fn(async(a:number,b:number)=>data.slice(a,b));
  const source={size:data.length,read} as unknown as UploadSource;
  expect(await verifyUploadSource(source,new AbortController().signal)).toBe(bytesToHex(sha256(data)));
  expect(read.mock.calls).toEqual([[0,1024*1024,expect.any(AbortSignal)],[1024*1024,data.length,expect.any(AbortSignal)]]);
 });
 it("refuses short reads",async()=>{
  const source={size:3,read:async()=>new Uint8Array(2)} as unknown as UploadSource;
  await expect(verifyUploadSource(source,new AbortController().signal)).rejects.toThrow("UPLOAD_FILE_UNAVAILABLE");
 });
 it("discards a read when cancelled",async()=>{
  const c=new AbortController();
  const source={size:3,read:async()=>{c.abort();return new Uint8Array(3)}} as unknown as UploadSource;
  await expect(verifyUploadSource(source,c.signal)).rejects.toThrow();
 });
});

it('allows scheduled navigation cancellation between synchronous native-like reads',async()=>{
 const c=new AbortController();let reads=0;
 const source={size:2*1024*1024,read:async(start:number,end:number)=>{
  reads++;if(reads===1)setTimeout(()=>c.abort(new Error('navigation')),0);
  return new Uint8Array(end-start);
 }} as unknown as UploadSource;
 await expect(verifyUploadSource(source,c.signal)).rejects.toThrow('navigation');expect(reads).toBe(1);
});
