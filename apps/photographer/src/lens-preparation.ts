import {lensJournalFence,lensJournalLock} from './lens-journal-lifecycle';
import {freezeCollectionVersionIntent,type CollectionVersionSelection} from './collection-version';
import type {V2VersionBinding} from './catalog-types';
import {freezeLensIntent,type LensChoice,type LensIntent} from './lens-client';
import {multiUUID} from './multi-intent';
import {renderInputSnapshotDigest,renderInputSnapshotRequest,renderInputSnapshotResult,type RenderInputSnapshotResult} from './render-input-snapshot';
export type LensPrepared={schema_version:1;occurrences?:Omit<CollectionVersionSelection,'version'>[];actor:string;project:string;snapshot_key:string;intent_key:string;sources:V2VersionBinding[];choices:LensChoice[];snapshot:RenderInputSnapshotResult|null;ticket:{run_id:string;workflow_id:string;state:'queued'}|null};
type Storage={getAllKeys?():Promise<readonly string[]>;getItem(key:string):Promise<string|null>;setItem(key:string,value:string):Promise<void>};
export class LensPreparationStore {
 private key:string;private fence:()=>Promise<void>;
 constructor(private storage:Storage,private origin:string,private actor:string,tenant:string,private project:string,private collection:string){
  if(![actor,tenant,project,collection].every(multiUUID)||!/^https?:\/\//.test(origin))throw new Error('LENS_JOURNAL_SCOPE_INVALID');
  this.fence=lensJournalFence(storage,origin);
  this.key=`photo.lens.preparation.v1:${JSON.stringify([origin,actor,tenant,project,collection])}`;
 }
 own(value:LensPrepared):LensPrepared{
  const keys=['schema_version','actor','project','snapshot_key','intent_key','sources','choices','snapshot','ticket',...(Object.hasOwn(value??{},'occurrences')?['occurrences']:[])];
  if(!value||typeof value!=='object'||Array.isArray(value)||Object.keys(value).length!==keys.length||!keys.every(key=>Object.hasOwn(value,key))||value.schema_version!==1||value.actor!==this.actor||value.project!==this.project||!multiUUID(value.snapshot_key)||!multiUUID(value.intent_key)||value.snapshot_key===value.intent_key)throw new Error('LENS_JOURNAL_INVALID');
  const sources=renderInputSnapshotRequest({sources:value.sources}).sources;
  const intent=freezeLensIntent({schema_version:1,snapshot_id:value.snapshot?.id??value.snapshot_key,choices:value.choices},sources.map(s=>s.asset_version_id));
  const snapshot=value.snapshot===null?null:renderInputSnapshotResult(value.snapshot,renderInputSnapshotDigest({sources}),sources.length);
  if(value.ticket!==null&&(!snapshot||Object.keys(value.ticket).sort().join(',')!=='run_id,state,workflow_id'||!multiUUID(value.ticket.run_id)||!multiUUID(value.ticket.workflow_id)||value.ticket.state!=='queued'))throw new Error('LENS_JOURNAL_INVALID');
  if(value.occurrences!==undefined&&!Array.isArray(value.occurrences))throw new Error('LENS_JOURNAL_INVALID');
  const occurrences=value.occurrences?.map(freezeCollectionVersionIntent);if(occurrences&&(occurrences.length!==sources.length||new Set(occurrences.map(x=>x.expectedVersion)).size!==sources.length||new Set(occurrences.map(x=>x.item)).size!==sources.length||occurrences.some(x=>x.collection!==this.collection||!sources.some(s=>s.asset_version_id===x.expectedVersion))))throw new Error('LENS_JOURNAL_INVALID');
  return {schema_version:1,...(occurrences?{occurrences}:{}),actor:this.actor,project:this.project,snapshot_key:value.snapshot_key,intent_key:value.intent_key,sources,choices:intent.choices,snapshot,ticket:value.ticket===null?null:structuredClone(value.ticket)};
 }
 async read(){await this.fence();const raw=await this.storage.getItem(this.key);await this.fence();if(raw===null||raw==='null')return null;if(raw.length>2_000_000)throw new Error('LENS_JOURNAL_INVALID');return this.own(JSON.parse(raw));}
 async archives(){await this.fence();if(!this.storage.getAllKeys)return [];const keys=await this.storage.getAllKeys();await this.fence();const prefix=this.key+':history:';const runs=keys.filter(key=>key.startsWith(prefix)).map(key=>key.slice(prefix.length));if(runs.length>1000||!runs.every(multiUUID))throw new Error('LENS_JOURNAL_INVALID');return runs.sort();}
 async readArchived(run:string){if(!multiUUID(run))throw new Error('LENS_JOURNAL_INVALID');await this.fence();const raw=await this.storage.getItem(`${this.key}:history:${run}`);await this.fence();if(raw===null||raw.length>2_000_000)throw new Error('LENS_JOURNAL_INVALID');const value=this.own(JSON.parse(raw));if(value.ticket?.run_id!==run)throw new Error('LENS_JOURNAL_INVALID');return value;}
 async archive(){return lensJournalLock(this.origin,async()=>{const value=await this.read();if(!value?.ticket)throw new Error('LENS_REQUEST_NOT_COMMITTED');await this.fence();await this.storage.setItem(`${this.key}:history:${value.ticket.run_id}`,JSON.stringify(value));await this.fence();await this.storage.setItem(this.key,'null');await this.fence();});}
 async write(value:LensPrepared){return lensJournalLock(this.origin,async()=>{await this.fence();const current=await this.read();if(current&&(current.intent_key!==value.intent_key||current.snapshot_key!==value.snapshot_key||JSON.stringify(current.sources)!==JSON.stringify(value.sources)||JSON.stringify(current.choices)!==JSON.stringify(value.choices)||JSON.stringify(current.occurrences??null)!==JSON.stringify(value.occurrences??null)||(current.ticket!==null&&JSON.stringify(current.ticket)!==JSON.stringify(value.ticket))||(current.snapshot!==null&&JSON.stringify(current.snapshot)!==JSON.stringify(value.snapshot))))throw new Error('LENS_JOURNAL_CONFLICT');const raw=JSON.stringify(this.own(value));if(raw.length>2_000_000)throw new Error('LENS_JOURNAL_INVALID');await this.fence();await this.storage.setItem(this.key,raw);await this.fence();});}
}
export async function beginLensPreparation(store:LensPreparationStore,actor:string,project:string,sources:V2VersionBinding[],choices:LensChoice[],occurrences?:Omit<CollectionVersionSelection,'version'>[]){
 const value:LensPrepared={schema_version:1,...(occurrences?{occurrences}:{}),actor,project,snapshot_key:crypto.randomUUID(),intent_key:crypto.randomUUID(),sources,choices,snapshot:null,ticket:null};
 await store.write(value);return store.own(value);
}
export class LensPreparation {
 private sending=false;
 constructor(private store:LensPreparationStore,private snapshot:(project:string,request:{sources:V2VersionBinding[]},key:string,signal?:AbortSignal)=>Promise<RenderInputSnapshotResult>,private submit:(intent:LensIntent,versions:readonly string[],key:string,signal?:AbortSignal)=>Promise<NonNullable<LensPrepared['ticket']>>){}
 async resume(signal?:AbortSignal){
  if(this.sending)throw new Error('LENS_PREPARATION_BUSY');this.sending=true;
  try{
   let value=await this.store.read();signal?.throwIfAborted();if(!value)throw new Error('LENS_JOURNAL_MISSING');
   if(value.ticket)return value;
   if(value.snapshot===null){const request=renderInputSnapshotRequest({sources:value.sources});const digest=renderInputSnapshotDigest(request);const receipt=await this.snapshot(value.project,request,value.snapshot_key,signal);value={...value,snapshot:renderInputSnapshotResult(receipt,digest,request.sources.length)};await this.store.write(value);}
   signal?.throwIfAborted();const body=freezeLensIntent({schema_version:1,snapshot_id:value.snapshot!.id,choices:value.choices},value.sources.map(s=>s.asset_version_id));
   const ticket=await this.submit(body,value.sources.map(s=>s.asset_version_id),value.intent_key,signal);
   // Persist a committed receipt even if navigation cancelled during the request;
   // the caller may hide it, but the immutable retry must not submit another key.
   value=this.store.own({...value,ticket});await this.store.write(value);signal?.throwIfAborted();return this.store.own(value);
  }finally{this.sending=false;}
 }
}
