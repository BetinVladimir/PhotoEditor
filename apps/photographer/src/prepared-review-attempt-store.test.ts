import {expect,it} from 'vitest';
import {PreparedReviewAttemptStore,clearPreparedReviewAttempts} from './prepared-review-attempt-store';
const id=(n:number)=>`${n.toString(16).padStart(8,'0')}-0000-4000-8000-000000000001`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const record=()=>({version:1 as const,key:id(8),body:{action:'revoke' as const,kind:'dust' as const,entry_id:id(5),target_version_id:id(6),proposal:ref(7),base:ref(9),expected_epoch:3}});
const receipt=()=>({workflow_id:id(4),entry_id:id(5),target_version_id:id(6),kind:'dust' as const,proposal_id:id(7),status:'revoked' as const,epoch:4});
const lock=async<T>(_origin:string,action:()=>Promise<T>)=>action();
function storage(){const values=new Map<string,string>();return {getItem:async(key:string)=>values.get(key)??null,setItem:async(key:string,value:string)=>{values.set(key,value);},removeItem:async(key:string)=>{values.delete(key);},getAllKeys:async()=>[...values.keys()]};}
it('restores an immutable attempt and requires exact acknowledged epoch to clear',async()=>{
 const data=storage(),store=new PreparedReviewAttemptStore(data,'origin',id(1),id(2),id(3),id(4),lock);
 await store.write(record());expect(await new PreparedReviewAttemptStore(data,'origin',id(1),id(2),id(3),id(4),lock).read()).toEqual(record());
 await expect(store.write({...record(),key:id(10)})).rejects.toThrow('CONFLICT');
 await expect(store.clear(id(8),{...receipt(),epoch:3})).rejects.toThrow('CONFLICT');expect(await store.read()).toEqual(record());
 await store.clear(id(8),receipt());expect(await store.read()).toBeNull();
});
it('isolates user/workflow and fences writers on logout',async()=>{
 const data=storage(),store=new PreparedReviewAttemptStore(data,'logout-origin',id(1),id(2),id(3),id(4),lock);await store.write(record());
 expect(await new PreparedReviewAttemptStore(data,'logout-origin',id(11),id(2),id(3),id(4),lock).read()).toBeNull();
 expect(await new PreparedReviewAttemptStore(data,'logout-origin',id(1),id(2),id(3),id(12),lock).read()).toBeNull();
 await clearPreparedReviewAttempts(data,'logout-origin',lock);await expect(store.write(record())).rejects.toThrow('EXPIRED');
});

it('persists the full supported 4096-candidate decision',async()=>{
 const data=storage(),store=new PreparedReviewAttemptStore(data,'maximum-origin',id(1),id(2),id(3),id(4),lock);
 const ids=Array.from({length:4096},(_,index)=>index.toString(16).padStart(64,'0'));
 const attempt={version:1 as const,key:id(8),body:{...record().body,action:'grant' as const,decision:'accept' as const,expected_epoch:0,accepted_candidate_ids:ids,rejected_candidate_ids:[]}};
 expect(JSON.stringify(attempt).length).toBeGreaterThan(262144);
 await store.write(attempt);expect(await store.read()).toEqual(attempt);
});
it('rejects oversized corrupted journal values before decoding',async()=>{
 const data=storage(),store=new PreparedReviewAttemptStore(data,'oversize-origin',id(1),id(2),id(3),id(4),lock);
 await data.setItem(store.key,' '.repeat((1<<20)+1));await expect(store.read()).rejects.toThrow('JOURNAL_INVALID');
});
