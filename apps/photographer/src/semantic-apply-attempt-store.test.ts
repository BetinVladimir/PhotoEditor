import {it,expect} from 'vitest';
import {SemanticApplyAttemptStore,clearSemanticApplyAttempts} from './semantic-apply-attempt-store';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const lock=async<T>(_origin:string,action:()=>Promise<T>)=>action();
function storage(){const map=new Map<string,string>();return {getItem:async(k:string)=>map.get(k)??null,setItem:async(k:string,v:string)=>{map.set(k,v)},removeItem:async(k:string)=>{map.delete(k)},getAllKeys:async()=>[...map.keys()]};}
const record=()=>({version:1 as const,key:id(1),workflow:id(2),body:{expected_revision:'1',plan_sha:'a'.repeat(64),target_decisions:[{target_id:id(3),decision:'approve' as const,preview:{render:{artifact_id:id(4),sha256:'b'.repeat(64)},evidence:{artifact_id:id(5),sha256:'c'.repeat(64)}}}]}});
it('recovers exact selected refs and cannot overwrite an uncertain operation',async()=>{
 const data=storage(),store=new SemanticApplyAttemptStore(data,'recovery',id(6),id(7),id(8),lock),value=record();
 await store.write(value);value.body.target_decisions[0].preview.render.sha256='d'.repeat(64);
 const resumed=new SemanticApplyAttemptStore(data,'recovery',id(6),id(7),id(8),lock);
 expect(await resumed.read()).toEqual(record());await expect(store.write(value)).rejects.toThrow('CONFLICT');
 await expect(store.clear(id(1),{workflow_id:id(2),revision:'3',remaining:0,batch_id:id(9),child:{run_id:id(10),state:'queued'}})).rejects.toThrow('CONFLICT');
 await store.clear(id(1),{workflow_id:id(2),revision:'2',remaining:0,batch_id:id(9),child:{run_id:id(10),state:'queued'}});
 expect(await store.read()).toBeNull();
});
it('isolates tenants and fences old writers after logout',async()=>{
 const data=storage(),store=new SemanticApplyAttemptStore(data,'logout',id(6),id(7),id(8),lock);
 await store.write(record());expect(await new SemanticApplyAttemptStore(data,'logout',id(6),id(9),id(8),lock).read()).toBeNull();
 await clearSemanticApplyAttempts(data,'logout',lock);await expect(store.write(record())).rejects.toThrow('EXPIRED');
});
it('fences another mounted writer after receipt and accepts a fresh operation',async()=>{
 const data=storage(),owner=new SemanticApplyAttemptStore(data,'receipt-fence',id(6),id(7),id(8),lock),stale=new SemanticApplyAttemptStore(data,'receipt-fence',id(6),id(7),id(8),lock);
 await owner.write(record());await stale.read();
 await owner.clear(id(1),{workflow_id:id(2),revision:'2',remaining:0,batch_id:id(9),child:{run_id:id(10),state:'queued'}});
 await expect(stale.write(record())).rejects.toThrow('EXPIRED');await expect(owner.write(record())).rejects.toThrow('EXPIRED');
 const fresh={...record(),key:id(11)};await owner.write(fresh);expect(await owner.read()).toEqual(fresh);
});
it('does not recover acknowledged request when removal fails after durable receipt marker',async()=>{
 const base=storage();let fail=true;
 const data={...base,removeItem:async(key:string)=>{if(fail)throw new Error('disk');await base.removeItem(key)}};
 const owner=new SemanticApplyAttemptStore(data,'receipt-crash',id(6),id(7),id(8),lock);await owner.write(record());
 await expect(owner.clear(id(1),{workflow_id:id(2),revision:'2',remaining:0,batch_id:id(9),child:{run_id:id(10),state:'queued'}})).rejects.toThrow('disk');
 fail=false;const restarted=new SemanticApplyAttemptStore(data,'receipt-crash',id(6),id(7),id(8),lock);
 expect(await restarted.read()).toBeNull();await expect(restarted.write(record())).rejects.toThrow('EXPIRED');
 const fresh={...record(),key:id(12)};await restarted.write(fresh);expect(await restarted.read()).toEqual(fresh);
});
