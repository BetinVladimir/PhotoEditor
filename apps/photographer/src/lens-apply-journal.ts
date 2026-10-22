import {lensJournalFence,lensJournalLock} from './lens-journal-lifecycle';
import type {LensProposal} from './lens-proposal';
import type {LensClient} from './lens-client';
import type {OwnerAPI} from './owner-api';
import {freezeCollectionVersionIntent,type CollectionVersionSelection} from './collection-version';
import {multiUUID} from './multi-intent';
type Storage={getItem(key:string):Promise<string|null>;setItem(key:string,value:string):Promise<void>};
const fields=(v:unknown,keys:string[]):v is {[key:string]:unknown}=>!!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.hasOwn(v,k));
type Applied={asset_version_id:string;parent_asset_version_id:string;source:{artifact_id:string;sha256:string}};
type Record={schema_version:1;proposal:LensProposal;selection:Omit<CollectionVersionSelection,'version'>;accepted:true;applied:Applied|null;selected:boolean};
/** Commit the exact photographer decision before the first mutation. Retries
 * reuse that root, then reconcile the same collection occurrence by CAS. */
export async function applyLensJournal(storage:Storage,key:string,origin:string,proposal:LensProposal,selection:Omit<CollectionVersionSelection,'version'>,lens:LensClient,owner:OwnerAPI,signal?:AbortSignal){
 const fence=lensJournalFence(storage,origin);await fence();
 const save=async(value:Record)=>lensJournalLock(origin,async()=>{await fence();await storage.setItem(key,JSON.stringify(value));await fence();});
 const frozen=structuredClone(proposal),target=freezeCollectionVersionIntent(selection);
 if(target.expectedVersion!==frozen.base.asset_version_id)throw new Error('LENS_SELECTION_INVALID');
 const raw=await storage.getItem(key);await fence();signal?.throwIfAborted();
 if(raw!==null&&raw.length>100000)throw new Error('LENS_APPLY_JOURNAL_INVALID');
 const decoded:unknown=raw===null?null:JSON.parse(raw);
 if(raw!==null&&!fields(decoded,['schema_version','proposal','selection','accepted','applied','selected']))throw new Error('LENS_APPLY_JOURNAL_INVALID');
 let record:Record=raw===null?{schema_version:1,proposal:frozen,selection:target,accepted:true,applied:null,selected:false}:decoded as Record;
 if(raw!==null&&(raw.length>100000||record.schema_version!==1||record.accepted!==true||typeof record.selected!=='boolean'||JSON.stringify(record.proposal)!==JSON.stringify(frozen)||JSON.stringify(record.selection)!==JSON.stringify(target)))throw new Error('LENS_APPLY_JOURNAL_INVALID');
 if(record.applied!==null){const result=record.applied;if(!fields(result,['asset_version_id','parent_asset_version_id','source'])||!fields(result.source,['artifact_id','sha256'])||!multiUUID(result.asset_version_id)||result.parent_asset_version_id!==target.expectedVersion||result.source?.artifact_id!==frozen.working_image.artifact_id||result.source.sha256!==frozen.working_image.sha256)throw new Error('LENS_APPLY_JOURNAL_INVALID');}
 else if(record.selected)throw new Error('LENS_APPLY_JOURNAL_INVALID');
 if(raw===null)await save(record);
 if(!record.applied){await fence();record={...record,applied:await lens.apply(frozen,true,signal)};await save(record);}
 signal?.throwIfAborted();
 if(!record.selected){
  if(record.applied!.asset_version_id!==target.expectedVersion){
   const change={...target,version:record.applied!.asset_version_id};
   await fence();const status=await owner.reconcileCollectionVersion(change,signal);await fence();
   if(status==='conflict')throw new Error('LENS_COLLECTION_CHANGED');
   if(status==='unchanged')await owner.replaceCollectionVersion(change,signal);
  }
  record={...record,selected:true};await save(record);
 }
 signal?.throwIfAborted();return record.applied!;
}
