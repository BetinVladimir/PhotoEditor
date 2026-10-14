import {expect,it} from 'vitest';
import {DeliveryPublishAttemptStore,clearDeliveryPublishAttempts} from './delivery-publish-attempt-store';
import type {DeliveryPackage} from './delivery-package';
import type {SemanticJournalLock} from './semantic-attempt-store';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const lock:SemanticJournalLock=async(_origin,action)=>action();
function storage(){const values=new Map<string,string>();return {getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)},getAllKeys:async()=>[...values.keys()]};}
const record=()=>({version:1 as const,key:id(8),body:{id:id(4),project_id:id(3),expected_revision:'2'}});
function published():DeliveryPackage{return {id:id(4),tenant_id:id(2),project_id:id(3),revision:'3',workflow_id:id(5),snapshot_id:id(6),name:'Delivery',policy_artifact_id:id(7),manifest_artifact_id:id(9),published_at:'2026-10-11T12:00:00Z',state:'ready',document:{artifact_id:id(9),sha256:'a'.repeat(64)}};}
it('restores original key and CAS across mounts without mutable aliases',async()=>{
 const data=storage(),first=new DeliveryPublishAttemptStore(data,'origin',id(1),id(2),id(3),id(4),lock),attempt=record();await first.write(attempt);attempt.body.expected_revision='7';
 const second=new DeliveryPublishAttemptStore(data,'origin',id(1),id(2),id(3),id(4),lock);
 const saved=await second.read();expect(saved).toEqual(record());saved!.body.expected_revision='8';expect(await second.read()).toEqual(record());
 await expect(second.write({...record(),key:id(10)})).rejects.toThrow('CONFLICT');
 await expect(second.write({...record(),body:{...record().body,expected_revision:'3'}})).rejects.toThrow('CONFLICT');
});
it('clears only the exact acknowledged publication and retains ambiguous attempts',async()=>{
 const data=storage(),store=new DeliveryPublishAttemptStore(data,'origin',id(1),id(2),id(3),id(4),lock);await store.write(record());
 for(const patch of [{id:id(10)},{tenant_id:id(10)},{project_id:id(10)},{revision:'4'},{published_at:null},{state:'review' as const},{document:null},{manifest_artifact_id:id(10)}]){
  await expect(store.clear(id(8),{...published(),...patch})).rejects.toThrow('CONFLICT');expect(await store.read()).toEqual(record());
 }
 await expect(store.clear(id(10),published())).rejects.toThrow('CONFLICT');
 await store.clear(id(8),published());expect(await store.read()).toBeNull();
});
it('isolates principals and fences stale writers after logout',async()=>{
 const data=storage(),store=new DeliveryPublishAttemptStore(data,'origin',id(1),id(2),id(3),id(4),lock);await store.write(record());
 expect(await new DeliveryPublishAttemptStore(data,'origin',id(10),id(2),id(3),id(4),lock).read()).toBeNull();
 expect(await new DeliveryPublishAttemptStore(data,'origin',id(1),id(2),id(3),id(11),lock).read()).toBeNull();
 await clearDeliveryPublishAttempts(data,'origin',lock);
 await expect(store.write(record())).rejects.toThrow('EXPIRED');
 expect(await new DeliveryPublishAttemptStore(data,'origin',id(1),id(2),id(3),id(4),lock).read()).toBeNull();
});

it('binds journal installation to screen scope before any storage access',()=>{
 const data={getItem:async()=>{throw new Error('unexpected IO')},setItem:async()=>{},removeItem:async()=>{}};
 const store=new DeliveryPublishAttemptStore(data,'scope-origin',id(1),id(2),id(3),id(4),lock);
 expect(()=>store.assertScope(id(2),id(3),id(4))).not.toThrow();
 for(const scope of [[id(90),id(3),id(4)],[id(2),id(90),id(4)],[id(2),id(3),id(90)]]){
  expect(()=>store.assertScope(scope[0],scope[1],scope[2])).toThrow('DELIVERY_PUBLISH_JOURNAL_SCOPE_INVALID');
 }
});


it('retains pending publication for malformed receipt metadata',async()=>{
 const data=storage(),store=new DeliveryPublishAttemptStore(data,'receipt-origin',id(1),id(2),id(3),id(4),lock);
 await store.write(record());
 for(const patch of [{published_at:'invalid'},{revision:'03'},{workflow_id:id(0).replace(/./g,'0')},{name:''},
  {document:{artifact_id:id(9),sha256:'invalid'}},{snapshot_id:'invalid'}]){
  await expect(store.clear(id(8),{...published(),...patch})).rejects.toThrow('CONFLICT');
  expect(await store.read()).toEqual(record());
 }
});
it('captures publication receipt before queued storage work',async()=>{
 const data=storage(),store=new DeliveryPublishAttemptStore(data,'receipt-copy-origin',id(1),id(2),id(3),id(4),lock);
 await store.write(record());
 const receipt=published(),clearing=store.clear(id(8),receipt);
 receipt.revision='999';receipt.document!.sha256='invalid';receipt.tenant_id=id(999);
 await clearing;expect(await store.read()).toBeNull();
});
it('cannot turn an invalid receipt into acknowledgement while queued',async()=>{
 const data=storage(),store=new DeliveryPublishAttemptStore(data,'receipt-invalid-copy-origin',id(1),id(2),id(3),id(4),lock);
 await store.write(record());
 const receipt={...published(),revision:'999'},clearing=store.clear(id(8),receipt);
 receipt.revision='3';
 await expect(clearing).rejects.toThrow('CONFLICT');expect(await store.read()).toEqual(record());
});


it('clears a confirmed package with a valid 200-code-point supplementary Unicode name',async()=>{
 const data=storage(),store=new DeliveryPublishAttemptStore(data,'unicode-receipt-origin',id(1),id(2),id(3),id(4),lock);
 await store.write(record());
 await store.clear(id(8),{...published(),name:'😀'.repeat(200)});
 expect(await store.read()).toBeNull();
});
