import {expect,it} from 'vitest';
import type {Review} from './catalog-types';
import {relightReviewDecisionIntent} from './relight-review-decision';
import {RelightReviewDecisionAttemptStore,clearRelightReviewDecisionAttempts} from './relight-review-decision-attempt-store';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const lock=async<T>(_origin:string,action:()=>Promise<T>)=>action();
function fixture(origin:string){
 const values=new Map<string,string>();
 const storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)},getAllKeys:async()=>[...values.keys()]};
 const review:Review={id:id(3),run_id:id(4),kind:'image.relight',state:'pending',revision:1,candidate_hash:'a'.repeat(64),candidate:{artifact_id:id(5),sha256:'a'.repeat(64)},base:{artifact_id:id(6),sha256:'b'.repeat(64)},reasons:[],created_at:'2026-10-16T00:00:00Z'};
 const body=relightReviewDecisionIntent(id(2),id(1),review,'accept'),record={version:1 as const,key:id(7),body};
 const receipt:Review={...review,state:'decided',revision:2,decision:'accept',decision_route:'candidate',decision_hash:body.decision_hash};
 const store=new RelightReviewDecisionAttemptStore(storage,origin,id(1),id(2),id(3),lock);
 return {storage,store,record,receipt,review};
}
it('restores the exact decision after reload and refuses replacement',async()=>{
 const f=fixture('reload');await f.store.write(f.record);
 const restored=new RelightReviewDecisionAttemptStore(f.storage,'reload',id(1),id(2),id(3),lock);
 expect(await restored.read()).toEqual(f.record);
 await expect(restored.write({...f.record,key:id(8)})).rejects.toThrow('CONFLICT');
 await expect(restored.write({...f.record,body:relightReviewDecisionIntent(id(2),id(1),f.review,'reject')})).rejects.toThrow('CONFLICT');
 expect(await restored.read()).toEqual(f.record);
});
it('clears only the exact actor-bound decided receipt',async()=>{
 const f=fixture('receipt');await f.store.write(f.record);
 await expect(f.store.clear(f.record.key,f.review)).rejects.toThrow('CONFLICT');
 await expect(f.store.clear(f.record.key,{...f.receipt,decision_hash:'c'.repeat(64)})).rejects.toThrow('CONFLICT');
 expect(await f.store.read()).toEqual(f.record);
 await f.store.clear(f.record.key,f.receipt);expect(await f.store.read()).toBeNull();
});
it('isolates user and review and expires the prior writer on logout',async()=>{
 const f=fixture('logout-decision');await f.store.write(f.record);
 expect(await new RelightReviewDecisionAttemptStore(f.storage,'logout-decision',id(8),id(2),id(3),lock).read()).toBeNull();
 expect(await new RelightReviewDecisionAttemptStore(f.storage,'logout-decision',id(1),id(2),id(8),lock).read()).toBeNull();
 await clearRelightReviewDecisionAttempts(f.storage,'logout-decision',lock);
 await expect(f.store.write(f.record)).rejects.toThrow('EXPIRED');
 await expect(f.store.clear(f.record.key,f.receipt)).rejects.toThrow('EXPIRED');
});
it('rejects cross-scope and oversized journal data',async()=>{
 const f=fixture('corrupt-decision');
 const foreign={...f.record,body:relightReviewDecisionIntent(id(2),id(9),f.review,'accept')};
 await f.storage.setItem(f.store.key,JSON.stringify(foreign));await expect(f.store.read()).rejects.toThrow('SCOPE_INVALID');
 await f.storage.setItem(f.store.key,' '.repeat((1<<20)+1));await expect(f.store.read()).rejects.toThrow('JOURNAL_INVALID');
});

it('keeps the durable decision when receipt removal fails',async()=>{
 const f=fixture('remove-failure');await f.store.write(f.record);
 const remove=f.storage.removeItem;
 f.storage.removeItem=async()=>{throw new Error('STORAGE_UNAVAILABLE')};
 await expect(f.store.clear(f.record.key,f.receipt)).rejects.toThrow('STORAGE_UNAVAILABLE');
 expect(await f.store.read()).toEqual(f.record);
 f.storage.removeItem=remove;
 await f.store.clear(f.record.key,f.receipt);expect(await f.store.read()).toBeNull();
});
it('rejects a persisted logout generation change from another context',async()=>{
 const origin='cross-context-generation',f=fixture(origin);await f.store.write(f.record);
 await f.storage.setItem('photo.relight-review-decision-generation.v1:'+JSON.stringify(origin),id(9));
 await expect(f.store.read()).rejects.toThrow('EXPIRED');
 await expect(f.store.write(f.record)).rejects.toThrow('EXPIRED');
 await expect(f.store.clear(f.record.key,f.receipt)).rejects.toThrow('EXPIRED');
 const fresh=new RelightReviewDecisionAttemptStore(f.storage,origin,id(1),id(2),id(3),lock);
 expect(await fresh.read()).toEqual(f.record);
});
