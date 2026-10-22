// @vitest-environment jsdom
import {beforeEach,afterEach,it,expect,vi} from 'vitest';
import {webcrypto,randomUUID} from 'node:crypto';
import {Buffer} from 'node:buffer';
const state=vi.hoisted(()=>({begin:vi.fn(),parts:vi.fn(),complete:vi.fn()}));
vi.mock('../../portal/src/lib/api',()=>({idempotencyKey:()=>crypto.randomUUID(),beginUpload:state.begin,signParts:state.parts,completeUpload:state.complete}));
import {uploadRecoverably} from '../../portal/src/lib/upload-recovery';
let scope='';
const id='11111111-1111-4111-8111-111111111111';
const upload={id,state:'uploading',revision:'1',filename:'photo.jpg',expected_size:4,part_bytes:16777216,parts_total:1,expires_at:'2099-01-01T00:00:00Z'};
const file=(bytes=[1,2,3,4])=>({name:'photo.jpg',size:4,arrayBuffer:async()=>new Uint8Array(bytes).buffer,slice:()=>new Blob([new Uint8Array(bytes)])}) as File;
beforeEach(()=>{sessionStorage.clear();scope=randomUUID();vi.clearAllMocks();vi.stubGlobal('crypto',{subtle:{digest:(algorithm:string,data:ArrayBuffer|ArrayBufferView)=>webcrypto.subtle.digest(algorithm,Buffer.from(ArrayBuffer.isView(data)?new Uint8Array(data.buffer,data.byteOffset,data.byteLength):new Uint8Array(data)))},randomUUID});state.begin.mockResolvedValue(upload);state.parts.mockResolvedValue({items:[{part_no:1,url:'https://example.invalid/part'}]});state.complete.mockResolvedValue({job_id:id,status:'queued'});vi.stubGlobal('fetch',vi.fn(async()=>new Response('',{headers:{etag:'exact-etag'}})));});
afterEach(()=>vi.unstubAllGlobals());
it('replays immutable begin key after Stop and late begin response, then confirms without repeating accepted completion',async()=>{
 let release!:(v:typeof upload)=>void;state.begin.mockImplementationOnce(()=>new Promise(r=>{release=r}));
 const controller=new AbortController();const first=uploadRecoverably(sessionStorage,scope,'csrf',file(),controller.signal,()=>true);
 const refused=expect(first).rejects.toThrow();await vi.waitFor(()=>expect(state.begin).toHaveBeenCalledTimes(1));
 const firstKey=state.begin.mock.calls[0][3];controller.abort();release(upload);await refused;
 expect(state.parts).not.toHaveBeenCalled();
 await uploadRecoverably(sessionStorage,scope,'csrf',file(),new AbortController().signal,()=>true);
 expect(state.begin.mock.calls[1][3]).toBe(firstKey);expect(state.complete).toHaveBeenCalledTimes(1);
 await uploadRecoverably(sessionStorage,scope,'csrf',file(),new AbortController().signal,()=>true);
 expect(state.begin).toHaveBeenCalledTimes(2);expect(state.complete).toHaveBeenCalledTimes(1);
});
it('uncertain complete retries identical completion key and parts without reuploading',async()=>{
 state.complete.mockRejectedValueOnce(new Error('ambiguous response'));
 await expect(uploadRecoverably(sessionStorage,scope,'csrf',file(),new AbortController().signal,()=>true)).rejects.toThrow();
 const args=state.complete.mock.calls[0];
 await uploadRecoverably(sessionStorage,scope,'csrf',file(),new AbortController().signal,()=>true);
 expect(state.complete.mock.calls[1][4]).toBe(args[4]);expect(state.complete.mock.calls[1][2]).toEqual(args[2]);
 expect(state.begin).toHaveBeenCalledTimes(1);expect(fetch).toHaveBeenCalledTimes(1);
});
it('different bytes cannot replace an uncertain file; a distinct guest session has independent scope',async()=>{
 state.begin.mockRejectedValueOnce(new Error('ambiguous'));
 await expect(uploadRecoverably(sessionStorage,scope,'csrf',file(),new AbortController().signal,()=>true)).rejects.toThrow();
 await expect(uploadRecoverably(sessionStorage,scope,'csrf',file([4,3,2,1]),new AbortController().signal,()=>true)).rejects.toThrow('RESELECT_ORIGINAL');
 expect(state.begin).toHaveBeenCalledTimes(1);
 await uploadRecoverably(sessionStorage,scope,'other-csrf',file(),new AbortController().signal,()=>true);
 expect(state.begin).toHaveBeenCalledTimes(2);
});
it('known accepted receipt survives failed durable write without another completion POST',async()=>{
 const storage={getItem:(key:string)=>sessionStorage.getItem(key),removeItem:(key:string)=>sessionStorage.removeItem(key),setItem:(key:string,value:string)=>{if(JSON.parse(value).accepted)throw Error('storage unavailable');sessionStorage.setItem(key,value);}} as Storage;
 await expect(uploadRecoverably(storage,scope,'csrf',file(),new AbortController().signal,()=>true)).rejects.toThrow('storage unavailable');
 await uploadRecoverably(storage,scope,'csrf',file(),new AbortController().signal,()=>true);
 expect(state.begin).toHaveBeenCalledTimes(1);expect(state.complete).toHaveBeenCalledTimes(1);
});
it.each(['unknown-field','null-upload-accepted','invalid-key','duplicate-parts','missing-receipt','oversized-etag'])('refuses corrupt durable upload journal: %s',async variant=>{
 state.begin.mockRejectedValueOnce(Error('ambiguous'));
 await expect(uploadRecoverably(sessionStorage,scope,'csrf',file(),new AbortController().signal,()=>true)).rejects.toThrow();
 const key=Object.keys(sessionStorage).find(k=>k.includes('upload-attempt'))!;
 const row=JSON.parse(sessionStorage.getItem(key)!);
 if(variant==='unknown-field')row.extra=true;
 if(variant==='null-upload-accepted'){row.accepted=true;row.receipt={job_id:id,status:'queued'};}
 if(variant==='invalid-key')row.beginKey='-'.repeat(36);
 if(variant==='missing-receipt'){row.upload=upload;row.parts=[{part_no:1,etag:'etag'}];row.accepted=true;}
 if(variant==='duplicate-parts'){row.upload=upload;row.parts=[{part_no:1,etag:'etag'},{part_no:1,etag:'etag'}];}
 if(variant==='oversized-etag'){row.upload=upload;row.parts=[{part_no:1,etag:'e'.repeat(1025)}];}
 sessionStorage.setItem(key,JSON.stringify(row));
 await expect(uploadRecoverably(sessionStorage,scope,'csrf',file(),new AbortController().signal,()=>true)).rejects.toThrow();
 expect(state.begin).toHaveBeenCalledTimes(1);expect(state.complete).not.toHaveBeenCalled();
});
