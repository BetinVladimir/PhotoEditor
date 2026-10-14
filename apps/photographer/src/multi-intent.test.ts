import {describe,it,expect} from 'vitest';
import {freezeMultiIntent,type MultiIntent,type MultiOperation} from './multi-intent';
import {MultiAttempt} from './multi-attempt';
import {MultiAttemptStore,clearMultiAttempts} from './multi-attempt-store';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const fixture=(operation:MultiOperation):MultiIntent=>({schema_version:1,snapshot_id:id(1),params:{schema_version:1,frames:[{asset_version_id:id(2),source:ref(3),canvas_hash:'b'.repeat(64)},{asset_version_id:id(4),source:ref(5),canvas_hash:'c'.repeat(64)}],reference_ordinal:1,max_pixels:200_000_000,calibration:ref(6),...(operation==='optical.panorama'?{projection:'planar' as const}:{motion_policy:'review' as const})}});
describe('multi intent and durable retry',()=>{
 for(const operation of ['optical.panorama','optical.focus_stack'] as const){
  it(`${operation} preserves ordered bindings and rejects authority overrides`,()=>{
   const original=fixture(operation),owned=freezeMultiIntent(operation,original);
   original.params.frames.reverse();original.params.calibration.sha256='d'.repeat(64);
   expect(owned.params.frames[0].asset_version_id).toBe(id(2));expect(owned.params.reference_ordinal).toBe(1);expect(owned.params.calibration.sha256).toBe('a'.repeat(64));
   for(const mutate of [
    (v:MultiIntent)=>{v.params.frames[1].asset_version_id=v.params.frames[0].asset_version_id;},
    (v:MultiIntent)=>{v.params.reference_ordinal=2;},
    (v:MultiIntent)=>{v.params.max_pixels=200_000_001;},
    (v:MultiIntent)=>{v.params.calibration=v.params.frames[0].source;},
    (v:MultiIntent)=>{Object.assign(v,{sources:[]});},
    (v:MultiIntent)=>{Object.assign(v.params,{runtime_lock_sha:'foreign'});},
   ]){const body=fixture(operation);mutate(body);expect(()=>freezeMultiIntent(operation,body)).toThrow();}
   const shared=fixture(operation);shared.params.frames[1].source=shared.params.frames[0].source;
   expect(()=>freezeMultiIntent(operation,shared)).not.toThrow(); // Different versions can retain the same original document.
  });
  it(`${operation} persists before IO and retries exact ordered request`,async()=>{
   const writes:unknown[]=[],sent:unknown[]=[];
   const attempt=new MultiAttempt(operation,id(7),'stable-key',fixture(operation),{write:async r=>{writes.push(r);}});
   await expect(attempt.submit(async(op,project,body,key)=>{expect(writes).toHaveLength(1);sent.push({op,project,body:structuredClone(body),key});body.params.frames.reverse();throw new Error('lost response');})).rejects.toThrow();
   expect(attempt.state).toBe('unknown');
   const restored=MultiAttempt.restore(attempt.snapshot,{write:async r=>{writes.push(r);}});
   await restored.submit(async(op,project,body,key)=>{sent.push({op,project,body,key});return {run_id:id(8),workflow_id:id(9),state:'queued'};});
   expect(sent[1]).toEqual(sent[0]);expect(restored.state).toBe('accepted');
   await restored.submit(async()=>{throw new Error('must not send accepted attempt');});
   const cancelled=new AbortController();cancelled.abort();
   await expect(restored.submit(async()=>{throw new Error('cancelled attempt sent');},cancelled.signal)).rejects.toThrow('MULTI_ATTEMPT_CANCELLED');
   expect(restored.state).toBe('accepted');
  });
 }
 it('separates operations and fences journal instances after logout',async()=>{
  const values=new Map<string,string>();const storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v);},removeItem:async(k:string)=>{values.delete(k);},getAllKeys:async()=>[...values.keys()]};
  const lock=async<T>(_origin:string,action:()=>Promise<T>)=>action();
  const panorama=new MultiAttemptStore(storage,'https://example.test',id(10),id(11),id(7),id(1),'optical.panorama',lock);
  const focus=new MultiAttemptStore(storage,'https://example.test',id(10),id(11),id(7),id(1),'optical.focus_stack',lock);
  const first=new MultiAttempt('optical.panorama',id(7),'first',fixture('optical.panorama'),panorama).snapshot;
  await panorama.write(first);expect(await focus.read()).toBeNull();
  await expect(panorama.clear('first')).rejects.toThrow();
  const changed=structuredClone(first);changed.body.params.frames.reverse();await expect(panorama.write(changed)).rejects.toThrow();
  const foreign=structuredClone(first);foreign.operation='optical.focus_stack';await expect(focus.write(foreign)).rejects.toThrow();
  await clearMultiAttempts(storage,'https://example.test',lock);
  await expect(panorama.read()).rejects.toThrow();await expect(focus.read()).rejects.toThrow();
  expect([...values.keys()].filter(k=>k.startsWith('photo.multi-attempt.v1:'))).toHaveLength(0);
 });
});
it.each(['optical.panorama','optical.focus_stack'] as const)('keeps the accepted %s ticket after caller cancellation',async operation=>{
 const signal=new AbortController(),ticket={run_id:id(8),workflow_id:id(9),state:'queued' as const},writes:unknown[]=[];
 const attempt=new MultiAttempt(operation,id(7),'stable-key',fixture(operation),{write:async record=>{writes.push(record);}});
 await expect(attempt.submit(async()=>{signal.abort();return ticket;},signal.signal)).rejects.toThrow('MULTI_ATTEMPT_CANCELLED');
 expect(attempt.state).toBe('accepted');expect((writes.at(-1) as {ticket:unknown}).ticket).toEqual(ticket);
 await expect(attempt.submit(async()=>{throw new Error('resubmitted');})).resolves.toEqual(ticket);
});
