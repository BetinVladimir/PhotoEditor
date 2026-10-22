import {describe,it,expect,vi} from 'vitest';
import {applyLensJournal} from './lens-apply-journal';
import type {LensProposal} from './lens-proposal';
import type {LensClient} from './lens-client';
import type {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const proposal:LensProposal={schema_version:1,tenant_id:id(1),project_id:id(2),producer_task_id:id(3),result_id:id(4),result:ref(5),base:{asset_version_id:id(6),source:ref(7),canvas_hash:'b'.repeat(64)},working_image:ref(8),changed_mask:ref(9),report:ref(10),canvas:{width:100,height:80,orientation:1,space_id:'lens:canvas',source_sha256:'c'.repeat(64)},status:'pass',requires_review:false};
const selection={collection:id(11),item:id(12),asset:id(13),revision:'1',expectedVersion:id(6)};
describe('durable selected Lens Apply',()=>{
 it('persists before mutation and recovers the receipt without creating another version',async()=>{
  const values=new Map<string,string>(),storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v);}};
  const apply=vi.fn(async()=>{expect(values.has('key')).toBe(true);return {asset_version_id:id(14),parent_asset_version_id:id(6),source:ref(8)};});
  let conflict=true;
  const replace=vi.fn(async()=>{}),reconcile=vi.fn(async()=>conflict?'conflict':'unchanged');
  const owner={reconcileCollectionVersion:reconcile,replaceCollectionVersion:replace} as unknown as OwnerAPI;
  await expect(applyLensJournal(storage,'key','https://lens.test',proposal,selection,{apply} as unknown as LensClient,owner)).rejects.toThrow('LENS_COLLECTION_CHANGED');
  expect(apply).toHaveBeenCalledTimes(1);expect(replace).not.toHaveBeenCalled();
  conflict=false;
  await expect(applyLensJournal(storage,'key','https://lens.test',proposal,selection,{apply} as unknown as LensClient,owner)).resolves.toMatchObject({asset_version_id:id(14)});
  expect(apply).toHaveBeenCalledTimes(1);expect(replace).toHaveBeenCalledTimes(1);
  await applyLensJournal(storage,'key','https://lens.test',proposal,selection,{apply} as unknown as LensClient,owner);
  expect(replace).toHaveBeenCalledTimes(1);
 });
 it('rejects a different selected root before replaying a saved decision',async()=>{
  let raw:string|null=null;const storage={getItem:async(k:string)=>k==='key'?raw:null,setItem:async(_:string,v:string)=>{raw=v;}};
  const apply=vi.fn(async()=>{throw new Error('response lost');});
  const owner={} as OwnerAPI,lens={apply} as unknown as LensClient;
  await expect(applyLensJournal(storage,'key','https://lens.test',proposal,selection,lens,owner)).rejects.toThrow('response lost');
  const changed=structuredClone(proposal);changed.result.sha256='b'.repeat(64);
  await expect(applyLensJournal(storage,'key','https://lens.test',changed,selection,lens,owner)).rejects.toThrow('LENS_APPLY_JOURNAL_INVALID');
  expect(apply).toHaveBeenCalledTimes(1);
 });
});

it('does not resurrect an Apply receipt when logout occurs during the server response',async()=>{
 const {clearLensAttempts}=await import('./lens-journal-lifecycle');
 const values=new Map<string,string>(),storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v);},removeItem:async(k:string)=>{values.delete(k);},getAllKeys:async()=>[...values.keys()]};
 const origin='https://lens-race.test',key='photo.lens.apply.v1:'+JSON.stringify([origin,id(1),id(2),id(3)])+':task:result';
 const apply=vi.fn(async()=>{await clearLensAttempts(storage,origin);return{asset_version_id:id(14),parent_asset_version_id:id(6),source:ref(8)};});
 const replace=vi.fn(),owner={reconcileCollectionVersion:vi.fn(),replaceCollectionVersion:replace} as unknown as OwnerAPI;
 await expect(applyLensJournal(storage,key,origin,proposal,selection,{apply} as unknown as LensClient,owner)).rejects.toThrow('LENS_JOURNAL_EXPIRED');
 expect(values.has(key)).toBe(false);expect(replace).not.toHaveBeenCalled();
});

it('withdraws before collection mutation if logout occurs during reconciliation',async()=>{
 const {clearLensAttempts}=await import('./lens-journal-lifecycle');
 const values=new Map<string,string>(),storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v);},removeItem:async(k:string)=>{values.delete(k);},getAllKeys:async()=>[...values.keys()]};
 const origin='https://lens-cas-race.test',key='photo.lens.apply.v1:'+JSON.stringify([origin,id(1),id(2),id(3)])+':task:result';
 const apply=vi.fn(async()=>({asset_version_id:id(14),parent_asset_version_id:id(6),source:ref(8)}));
 const replace=vi.fn(),owner={reconcileCollectionVersion:async()=>{await clearLensAttempts(storage,origin);return 'unchanged';},replaceCollectionVersion:replace} as unknown as OwnerAPI;
 await expect(applyLensJournal(storage,key,origin,proposal,selection,{apply} as unknown as LensClient,owner)).rejects.toThrow('LENS_JOURNAL_EXPIRED');
 expect(values.has(key)).toBe(false);expect(replace).not.toHaveBeenCalled();
});
