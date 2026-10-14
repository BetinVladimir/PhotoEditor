import {it,expect} from 'vitest';
import {submitPerspectiveDetection,recoverPerspectiveDetectionSnapshot,recoverPerspectiveDetectionReceipt} from './perspective-detection-submit';
import type {PerspectiveLinesSubmissionIntent} from './perspective-lines-submission';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const request=():PerspectiveLinesSubmissionIntent=>({schema_version:1,snapshot_id:id(3),base:{asset_version_id:id(4),source:{artifact_id:id(5),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)}});
const receipt={run_id:id(6),workflow_id:id(7),task_id:id(8),state:'queued' as const};
it('persists before POST and retries a lost response with the same key',async()=>{
 let saved:string|null=null;const keys:string[]=[];let fail=true;
 const journal={getItem:async()=>saved,setItem:async(_key:string,value:string)=>{saved=value;}};
 const api={submitPerspectiveLineDetection:async(_p:string,_b:PerspectiveLinesSubmissionIntent,key:string)=>{expect(saved).not.toBeNull();keys.push(key);if(fail){fail=false;throw new Error('lost response');}return receipt;}};
 const invoke=()=>submitPerspectiveDetection(api,journal,'journal',id(1),id(2),request(),()=>id(9),new AbortController().signal);
 await expect(invoke()).rejects.toThrow();await expect(invoke()).resolves.toEqual(receipt);expect(keys).toEqual([id(9),id(9)]);
});
it('does not submit when journal persistence fails or cancellation precedes work',async()=>{
 let calls=0;const api={submitPerspectiveLineDetection:async()=>{calls++;return receipt;}};
 const journal={getItem:async()=>null,setItem:async()=>{throw new Error('storage');}};
 await expect(submitPerspectiveDetection(api,journal,'j',id(1),id(2),request(),()=>id(9),new AbortController().signal)).rejects.toThrow();expect(calls).toBe(0);
 const c=new AbortController();c.abort();await expect(submitPerspectiveDetection(api,journal,'j',id(1),id(2),request(),()=>id(9),c.signal)).rejects.toThrow();expect(calls).toBe(0);
});

it('recovers the saved snapshot only for the exact current source and refuses corrupt journals',async()=>{
 const body=request();const value=JSON.stringify({schema_version:1,key:id(9),body});
 const journal={getItem:async()=>value,setItem:async()=>{}};
 expect(await recoverPerspectiveDetectionSnapshot(journal,'j',id(1),id(2),body.base,new AbortController().signal)).toBe(id(3));
 const changed=structuredClone(body.base);changed.source.sha256='c'.repeat(64);
 await expect(recoverPerspectiveDetectionSnapshot(journal,'j',id(1),id(2),changed,new AbortController().signal)).rejects.toThrow();
 expect(await recoverPerspectiveDetectionSnapshot({getItem:async()=>null,setItem:async()=>{}},'j',id(1),id(2),body.base,new AbortController().signal)).toBeNull();
 for(const bad of ['null','{}',JSON.stringify({schema_version:1,key:id(9),body,extra:true})]){
 await expect(recoverPerspectiveDetectionSnapshot({getItem:async()=>bad,setItem:async()=>{}},'j',id(1),id(2),body.base,new AbortController().signal)).rejects.toThrow();
 }
});
it('rejects a saved snapshot returned after cancellation',async()=>{
 const c=new AbortController();const body=request();
 const journal={getItem:async()=>{c.abort();return JSON.stringify({schema_version:1,key:id(9),body});},setItem:async()=>{}};
 await expect(recoverPerspectiveDetectionSnapshot(journal,'j',id(1),id(2),body.base,c.signal)).rejects.toThrow();
});

it('persists the receipt and recovers producer without another POST',async()=>{
 let saved:string|null=null;let calls=0;
 const journal={getItem:async()=>saved,setItem:async(_key:string,value:string)=>{saved=value;}};
 const api={submitPerspectiveLineDetection:async()=>{calls++;return receipt;}};
 const invoke=()=>submitPerspectiveDetection(api,journal,'j',id(1),id(2),request(),()=>id(9),new AbortController().signal);
 await invoke();await invoke();expect(calls).toBe(1);
 expect(await recoverPerspectiveDetectionReceipt(journal,'j',id(1),id(2),request(),new AbortController().signal)).toEqual(receipt);
 const changed=request();changed.snapshot_id=id(10);
 await expect(recoverPerspectiveDetectionReceipt(journal,'j',id(1),id(2),changed,new AbortController().signal)).rejects.toThrow();
});
it('keeps the original key when receipt persistence fails after admission',async()=>{
 let saved:string|null=null;let writes=0;const keys:string[]=[];
 const journal={getItem:async()=>saved,setItem:async(_key:string,value:string)=>{writes++;if(writes===2)throw new Error('storage');saved=value;}};
 const api={submitPerspectiveLineDetection:async(_p:string,_b:PerspectiveLinesSubmissionIntent,key:string)=>{keys.push(key);return receipt;}};
 const invoke=()=>submitPerspectiveDetection(api,journal,'j',id(1),id(2),request(),()=>id(9),new AbortController().signal);
 await expect(invoke()).rejects.toThrow();await invoke();expect(keys).toEqual([id(9),id(9)]);
});

it('concurrent editors in one runtime share one persisted key and one POST',async()=>{
 let saved:string|null=null;let calls=0;let keys=0;
 const journal={getItem:async()=>saved,setItem:async(_key:string,value:string)=>{saved=value;}};
 const api={submitPerspectiveLineDetection:async()=>{calls++;return receipt;}};
 const invoke=()=>submitPerspectiveDetection(api,journal,'shared',id(1),id(2),request(),()=>{keys++;return id(9+keys);},new AbortController().signal);
 const values=await Promise.all([invoke(),invoke()]);expect(values).toEqual([receipt,receipt]);expect(calls).toBe(1);expect(keys).toBe(1);
});
