import {describe,it,expect} from 'vitest';
import {PerspectiveLinesAttempt} from './perspective-lines-attempt';
import {PerspectiveLinesAttemptStore,clearPerspectiveLinesAttempts} from './perspective-lines-attempt-store';
import type {PerspectiveLinesIntent,PerspectiveLinesReceipt} from './perspective-lines';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const body=():PerspectiveLinesIntent=>({schema_version:1,snapshot_id:id(1),base:{asset_version_id:id(2),source:{artifact_id:id(3),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)},line_segments:{schema_version:1,lines:[{a:{x:0,y:.2},b:{x:1,y:.2},family:'horizontal'},{a:{x:0,y:.8},b:{x:1,y:.8},family:'horizontal'},{a:{x:.2,y:0},b:{x:.2,y:1},family:'vertical'},{a:{x:.8,y:0},b:{x:.8,y:1},family:'vertical'}]}});
const receipt=(request:PerspectiveLinesIntent):PerspectiveLinesReceipt=>({schema_version:1,tenant_id:id(4),project_id:id(5),snapshot_id:request.snapshot_id,base:request.base,lines:{artifact_id:id(6),sha256:'c'.repeat(64)}});
describe('durable perspective lines attempt',()=>{
 it('persists before IO and retries the same body/key after an unknown response',async()=>{
 const writes:unknown[]=[],sent:unknown[]=[];const original=body();
 const attempt=new PerspectiveLinesAttempt(id(4),id(5),'lines-key',original,{write:async record=>{writes.push(record);}});
 original.line_segments.lines[0].a.x=.4;
 await expect(attempt.submit(async(project,request,key)=>{expect(writes.length).toBe(1);sent.push({project,request,key});throw new Error('lost');})).rejects.toThrow('lost');
 expect(attempt.state).toBe('unknown');
 await attempt.submit(async(project,request,key)=>{sent.push({project,request,key});return receipt(request);});
 expect(sent[1]).toEqual(sent[0]);expect(attempt.state).toBe('accepted');
 await attempt.submit(async()=>{throw new Error('cached receipt sent again');});
 });
 it('retains unresolved attempts and fences stale stores after logout',async()=>{
 const data=new Map<string,string>();
 const storage={getItem:async(k:string)=>data.get(k)??null,setItem:async(k:string,v:string)=>{data.set(k,v);},removeItem:async(k:string)=>{data.delete(k);},getAllKeys:async()=>[...data.keys()]};
 const lock=async<T>(_origin:string,action:()=>Promise<T>)=>action();
 const store=new PerspectiveLinesAttemptStore(storage,'https://photo.test',id(7),id(4),id(5),id(2),lock);
 const attempt=new PerspectiveLinesAttempt(id(4),id(5),'lines-key',body(),store);
 await store.write(attempt.snapshot);
 await expect(store.clear('lines-key')).rejects.toThrow('CONFLICT');
 await expect(store.write({...attempt.snapshot,key:'another'})).rejects.toThrow('CONFLICT');
 await clearPerspectiveLinesAttempts(storage,'https://photo.test',lock);
 await expect(store.read()).rejects.toThrow('EXPIRED');
 });
 it('owns the accepted lines receipt across a mutating persistence callback',async()=>{
  const expected=receipt(body());
  const attempt=new PerspectiveLinesAttempt(id(4),id(5),'lines-key',body(),{write:async record=>{if(record.receipt)record.receipt.lines.sha256='d'.repeat(64);}});
  await expect(attempt.submit(async()=>expected)).resolves.toEqual(expected);
  expect(attempt.snapshot.receipt).toEqual(expected);
 });
});
