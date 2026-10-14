import {expect,it} from 'vitest';
import {NegativeAttempt} from './negative-attempt';
import {NegativeAttemptStore,clearNegativeAttempts} from './negative-attempt-store';
import type {NegativeIntent} from './negative-intent';
const id=(n:number)=>`00000000-0000-4000-8000-${n.toString().padStart(12,'0')}`;
const body:NegativeIntent={schema_version:1,snapshot_id:id(1),params:{schema_version:1,base:{asset_version_id:id(2),source:{artifact_id:id(3),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)},strength:1,film_base_rgb:[1,.8,.6],calibration:{artifact_id:id(4),sha256:'c'.repeat(64)},exposure_ev:0}};
const lock=async<T,>(_origin:string,action:()=>Promise<T>)=>action();
function memory(){const values=new Map<string,string>();return {getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)},getAllKeys:async()=>[...values.keys()]};}
it('rejects a different body/key until the acknowledged attempt is cleared',async()=>{
 const storage=memory(),store=new NegativeAttemptStore(storage,'https://negative.test',id(8),id(9),id(5),id(2),lock);
 const attempt=new NegativeAttempt(id(5),'first',body,store);await store.write(attempt.snapshot);
 expect((await store.read())?.key).toBe('first');
 const changed={...attempt.snapshot,key:'second'};await expect(store.write(changed)).rejects.toThrow('NEGATIVE_JOURNAL_CONFLICT');
 await expect(store.clear('first')).rejects.toThrow('NEGATIVE_JOURNAL_CONFLICT');
 await attempt.submit(async()=>({run_id:id(6),workflow_id:id(7),state:'queued'}));await store.clear('first');expect(await store.read()).toBeNull();
});
it('logout fences existing stores and clears only the matching origin',async()=>{
 const storage=memory(),store=new NegativeAttemptStore(storage,'https://negative.test',id(8),id(9),id(5),id(2),lock),other=new NegativeAttemptStore(storage,'https://other.test',id(8),id(9),id(5),id(2),lock);
 const attempt=new NegativeAttempt(id(5),'first',body,store),second=new NegativeAttempt(id(5),'other',body,other);
 await store.write(attempt.snapshot);await other.write(second.snapshot);
 await clearNegativeAttempts(storage,'https://negative.test',lock);
 await expect(store.write(attempt.snapshot)).rejects.toThrow('NEGATIVE_JOURNAL_EXPIRED');expect((await other.read())?.key).toBe('other');
});
