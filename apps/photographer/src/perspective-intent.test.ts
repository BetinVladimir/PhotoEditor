import {describe,it,expect} from 'vitest';
import {freezePerspectiveIntent,type PerspectiveIntent} from './perspective-intent';
import {PerspectiveAttempt} from './perspective-attempt';
import {PerspectiveAttemptStore,clearPerspectiveAttempts} from './perspective-attempt-store';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const fixture=():PerspectiveIntent=>({schema_version:1,snapshot_id:id(1),params:{schema_version:1,base:{asset_version_id:id(2),source:{artifact_id:id(3),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)},strength:1,mode:'manual',corners:[{x:0,y:0},{x:1,y:0},{x:1,y:1},{x:0,y:1}],lines:null,max_crop_fraction:.5}});
describe('perspective intent and durable retry',()=>{
 it('owns inputs and rejects malformed geometry and authority overrides',()=>{
  const original=fixture(),owned=freezePerspectiveIntent(original);original.params.corners![0].x=.2;expect(owned.params.corners![0].x).toBe(0);
  for(const mutate of [(v:PerspectiveIntent)=>{v.params.corners![1]={x:0,y:1};},(v:PerspectiveIntent)=>{v.params.strength=NaN;},(v:PerspectiveIntent)=>{v.params.lines={artifact_id:id(4),sha256:'c'.repeat(64)};},(v:PerspectiveIntent)=>{Object.assign(v,{qualification:{}});}]){const v=fixture();mutate(v);expect(()=>freezePerspectiveIntent(v)).toThrow();}
 });
 it('persists before IO and retries the exact key/body after an unknown result',async()=>{
  const writes:unknown[]=[],sent:unknown[]=[];
  const attempt=new PerspectiveAttempt(id(5),'perspective-key',fixture(),{write:async record=>{writes.push(record);}});
  await expect(attempt.submit(async(project,body,key)=>{sent.push({project,body,key});expect(writes).toHaveLength(1);throw new Error('connection lost');})).rejects.toThrow();
  expect(attempt.state).toBe('unknown');
  const ticket={run_id:id(6),workflow_id:id(7),state:'queued' as const};
  await attempt.submit(async(project,body,key)=>{sent.push({project,body,key});return ticket;});
  expect(sent[1]).toEqual(sent[0]);expect(attempt.state).toBe('accepted');
  await attempt.submit(async()=>{throw new Error('must not send cached receipt');});
  const cancelled=new AbortController();cancelled.abort();
  await expect(attempt.submit(async()=>{throw new Error('cancelled send');},cancelled.signal)).rejects.toThrow('PERSPECTIVE_ATTEMPT_CANCELLED');
 });
 it('blocks replacing unresolved attempts and fences stores on logout',async()=>{
  const values=new Map<string,string>();const storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v);},removeItem:async(k:string)=>{values.delete(k);},getAllKeys:async()=>[...values.keys()]};
  const lock=async<T>(_origin:string,action:()=>Promise<T>)=>action();
  const store=new PerspectiveAttemptStore(storage,'https://example.test',id(8),id(9),id(5),id(2),lock);
  const first=new PerspectiveAttempt(id(5),'first',fixture(),store).snapshot;
  await store.write(first);await expect(store.clear('first')).rejects.toThrow();
  const second=new PerspectiveAttempt(id(5),'second',fixture(),store).snapshot;
  await expect(store.write(second)).rejects.toThrow();
  await clearPerspectiveAttempts(storage,'https://example.test',lock);
  await expect(store.read()).rejects.toThrow();
 });
});
it('keeps the accepted perspective ticket after caller cancellation',async()=>{
 const signal=new AbortController(),ticket={run_id:id(6),workflow_id:id(7),state:'queued' as const},writes:unknown[]=[];
 const attempt=new PerspectiveAttempt(id(5),'perspective-key',fixture(),{write:async record=>{writes.push(record);}});
 await expect(attempt.submit(async()=>{signal.abort();return ticket;},signal.signal)).rejects.toThrow('PERSPECTIVE_ATTEMPT_CANCELLED');
 expect(attempt.state).toBe('accepted');expect((writes.at(-1) as {ticket:unknown}).ticket).toEqual(ticket);
 await expect(attempt.submit(async()=>{throw new Error('resubmitted');})).resolves.toEqual(ticket);
});
