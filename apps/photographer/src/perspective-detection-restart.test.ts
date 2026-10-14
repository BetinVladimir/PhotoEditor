import {APIError} from '@photoeditor/api-client';
import {it,expect} from 'vitest';
import {restartPerspectiveDetection,PerspectiveDetectionAdmissionRecovered} from './perspective-detection-restart';
import type {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const body={schema_version:1 as const,snapshot_id:id(3),base:{asset_version_id:id(4),source:{artifact_id:id(5),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)}};
const receipt={run_id:id(6),workflow_id:id(7),task_id:id(8),state:'queued'};
it('rechecks terminal state and archives old journal before preparing a new key/snapshot',async()=>{
 let saved=JSON.stringify({schema_version:2,key:id(9),body,receipt});const original=saved;const writes:string[]=[];
 const journal={getItem:async()=>saved,setItem:async(key:string,value:string)=>{writes.push(key);if(key==='j')saved=value;}};
 const api={get:async()=>({run_id:id(6),workflow_id:id(7),state:'failed'}),createRenderInputSnapshot:async()=>({id:id(10),digest:'c'.repeat(64),item_count:1})} as unknown as Pick<OwnerAPI,'get'|'perspectiveDetectedLines'|'createRenderInputSnapshot'>;
 const next=await restartPerspectiveDetection(api,journal,'j',id(1),id(2),body,()=>id(11),new AbortController().signal);
 expect(next.snapshot_id).toBe(id(10));expect(JSON.parse(saved).key).toBe(id(11));expect(writes).toEqual(['j:history:'+id(6),'j']);expect(original).not.toBe(saved);
});
it('never replaces a pending or cancelling attempt',async()=>{
 for(const state of ['queued','cancelling']){
 let writes=0;const journal={getItem:async()=>JSON.stringify({schema_version:2,key:id(9),body,receipt}),setItem:async()=>{writes++;}};
 const api={get:async()=>({run_id:id(6),workflow_id:id(7),state}),createRenderInputSnapshot:async()=>{throw new Error('unexpected snapshot');}} as unknown as Pick<OwnerAPI,'get'|'perspectiveDetectedLines'|'createRenderInputSnapshot'>;
 await expect(restartPerspectiveDetection(api,journal,'j',id(1),id(2),body,()=>id(11),new AbortController().signal)).rejects.toThrow('PERSPECTIVE_DETECTION_NOT_TERMINAL');expect(writes).toBe(0);
 }
});

it('rechecks a receipt-less draft using its original key before restarting',async()=>{
 let saved=JSON.stringify({schema_version:1,key:id(9),body});const calls:string[]=[];
 const journal={getItem:async()=>saved,setItem:async(key:string,value:string)=>{calls.push(key);if(key==='j')saved=value;}};
 const api={submitPerspectiveLineDetection:async(_project:string,request:unknown,key:string)=>{expect(request).toEqual(body);expect(key).toBe(id(9));throw new APIError(410,'PERSPECTIVE_DETECTION_DRAFT_EXPIRED');},createRenderInputSnapshot:async()=>({id:id(10)})} as unknown as Parameters<typeof restartPerspectiveDetection>[0];
 const next=await restartPerspectiveDetection(api,journal,'j',id(1),id(2),body,()=>id(11),new AbortController().signal);
 expect(next.snapshot_id).toBe(id(10));expect(calls).toEqual(['j:history:draft:'+id(9),'j']);
});
it('keeps the original journal for generic expiry and transport uncertainty',async()=>{
 for(const error of [new APIError(410,'SNAPSHOT_EXPIRED'),new APIError(503,'DEPENDENCY_UNAVAILABLE'),new Error('timeout')]){
 const original=JSON.stringify({schema_version:1,key:id(9),body});let writes=0;
 const journal={getItem:async()=>original,setItem:async()=>{writes++;}};
 const api={submitPerspectiveLineDetection:async()=>{throw error;},createRenderInputSnapshot:async()=>{throw new Error('unexpected snapshot');}} as unknown as Parameters<typeof restartPerspectiveDetection>[0];
 await expect(restartPerspectiveDetection(api,journal,'j',id(1),id(2),body,()=>id(11),new AbortController().signal)).rejects.toBe(error);expect(writes).toBe(0);
 }
});
it('persists recovered admission and refuses to rotate its key',async()=>{
 let saved=JSON.stringify({schema_version:1,key:id(9),body});
 const journal={getItem:async()=>saved,setItem:async(_key:string,value:string)=>{saved=value;}};
 const api={submitPerspectiveLineDetection:async()=>receipt,createRenderInputSnapshot:async()=>{throw new Error('unexpected snapshot');}} as unknown as Parameters<typeof restartPerspectiveDetection>[0];
 await expect(restartPerspectiveDetection(api,journal,'j',id(1),id(2),body,()=>id(11),new AbortController().signal)).rejects.toThrow('PERSPECTIVE_DETECTION_NOT_TERMINAL');
 expect(JSON.parse(saved)).toEqual({schema_version:2,key:id(9),body,receipt});
});

it('returns an owned validated recovered admission for immediate editor polling',()=>{
 const value={...receipt};const recovered=new PerspectiveDetectionAdmissionRecovered(value);
 value.task_id=id(99);
 expect(recovered.receipt.task_id).toBe(id(8));expect(Object.isFrozen(recovered.receipt)).toBe(true);
 expect(()=>new PerspectiveDetectionAdmissionRecovered({...receipt,state:'draft'})).toThrow();
});
it('rejects malformed successful replay without creating a new snapshot',async()=>{
 let saved=JSON.stringify({schema_version:1,key:id(9),body});const original=saved;
 const journal={getItem:async()=>saved,setItem:async(_key:string,value:string)=>{saved=value;}};
 const api={submitPerspectiveLineDetection:async()=>undefined,createRenderInputSnapshot:async()=>{throw new Error('unexpected snapshot');}} as unknown as Parameters<typeof restartPerspectiveDetection>[0];
 await expect(restartPerspectiveDetection(api,journal,'j',id(1),id(2),body,()=>id(11),new AbortController().signal)).rejects.toThrow();
 expect(saved).toBe(original);
});
