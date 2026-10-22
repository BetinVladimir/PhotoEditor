import {decodeSemanticEditIntent,decodeSemanticJobTicket,type SemanticEditIntent,type SemanticJobTicket} from '@photoeditor/api-client';
import type {DraftStorage} from './capture-draft-store';
import type {ArtifactRef,V2VersionBinding} from './catalog-types';
export type StoredSemanticAttempt={schema_version:1;snapshotKey:string;proposalKey:string;collection:{id:string;revision:string};mask:ArtifactRef;kind:SemanticEditIntent['kind'];strength:number;ev:number|null;prompt:string|null;binding:V2VersionBinding|null;snapshot:string|null;body:SemanticEditIntent|null;ticket:SemanticJobTicket|null};
const prefix='photo.semantic-attempt.v1:',uuid=/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const nonzero=(v:unknown):v is string=>typeof v==='string'&&uuid.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const epochs=new Map<string,number>();let pending:Promise<unknown>=Promise.resolve();
function queue<T>(action:()=>Promise<T>){const next=pending.catch(()=>{}).then(action);pending=next;return next}
export type SemanticJournalLock=<T>(origin:string,action:()=>Promise<T>)=>Promise<T>;
export const semanticJournalLock:SemanticJournalLock=async(origin,action)=>{
 if(typeof document==='undefined')return action();
 const locks=globalThis.navigator?.locks;
 if(!locks)throw new Error('SEMANTIC_JOURNAL_LOCK_UNAVAILABLE');
 return locks.request('photo.semantic-journal:'+origin,{mode:'exclusive'},action);
};
const generationKey=(origin:string)=>'photo.semantic-generation.v1:'+JSON.stringify(origin);
function decode(raw:string,version:string,collection:string):StoredSemanticAttempt{
 if(raw.length>32768)throw new Error('SEMANTIC_ATTEMPT_INVALID');
 const v=JSON.parse(raw) as StoredSemanticAttempt;
 if(!v||Object.keys(v).sort().join(',')!=='binding,body,collection,ev,kind,mask,prompt,proposalKey,schema_version,snapshot,snapshotKey,strength,ticket'||v.schema_version!==1||!nonzero(v.snapshotKey)||!nonzero(v.proposalKey)||v.snapshotKey===v.proposalKey||!v.collection||Object.keys(v.collection).sort().join(',')!=='id,revision'||v.collection.id!==collection||typeof v.collection.revision!=='string'||!/^[1-9][0-9]{0,18}$/.test(v.collection.revision)||BigInt(v.collection.revision)>9223372036854775807n||v.snapshot!==null&&!nonzero(v.snapshot))throw new Error('SEMANTIC_ATTEMPT_INVALID');
 // Validate every request field even before source/snapshot reads complete.
 const placeholder={asset_version_id:version,source:v.mask,canvas_hash:'a'.repeat(64)};
 const expected=decodeSemanticEditIntent({schema_version:1,kind:v.kind,reference:v.binding??placeholder,reference_mask:v.mask,target_snapshot_id:v.snapshot??collection,strength:v.strength,ev:v.ev,preserve_persons:true,prompt:v.prompt});
 if(expected.reference.asset_version_id!==version||v.snapshot!==null&&v.binding===null||v.body!==null&&(v.snapshot===null||JSON.stringify(decodeSemanticEditIntent(v.body))!==JSON.stringify(expected))||v.ticket!==null&&v.body===null)throw new Error('SEMANTIC_ATTEMPT_INVALID');
 if(v.ticket!==null)decodeSemanticJobTicket(v.ticket);
 return structuredClone(v);
}
/** Only a fully decoded immutable journal body can bypass a live admission hint. */
export function hasSemanticReplayBody(value:StoredSemanticAttempt,version:string,collection:string):boolean{
 try{return decode(JSON.stringify(value),version,collection).body!==null}catch{return false}
}
export class SemanticAttemptStore{
 readonly key:string;private epoch:number;private generation:string|null|undefined;
 constructor(private storage:DraftStorage,private origin:string,user:string,tenant:string,asset:string,private version:string,private collection:string,private lock:SemanticJournalLock=semanticJournalLock){
  if(!origin||![user,tenant,asset,version,collection].every(nonzero))throw new Error('SEMANTIC_ATTEMPT_SCOPE_INVALID');
  this.key=prefix+JSON.stringify([origin,user,tenant,asset,version,collection]);this.epoch=epochs.get(origin)??0;
 }
 private current(){if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('SEMANTIC_ATTEMPT_SCOPE_EXPIRED')}
 private async fence(){this.current();const generation=await this.storage.getItem(generationKey(this.origin));this.current();if(generation!==null&&!nonzero(generation))throw new Error('SEMANTIC_ATTEMPT_INVALID');if(this.generation===undefined)this.generation=generation;else if(this.generation!==generation)throw new Error('SEMANTIC_ATTEMPT_SCOPE_EXPIRED')}
 private transaction<T>(action:()=>Promise<T>){return queue(()=>this.lock(this.origin,action))}
 read(){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.key);this.current();return raw===null?null:decode(raw,this.version,this.collection)})}
 write(value:StoredSemanticAttempt){const raw=JSON.stringify(decode(JSON.stringify(value),this.version,this.collection));return this.transaction(async()=>{
  await this.fence();const previous=await this.storage.getItem(this.key);this.current();
  if(previous!==null){const old=decode(previous,this.version,this.collection),next=JSON.parse(raw) as StoredSemanticAttempt;
   for(const key of ['snapshotKey','proposalKey','collection','mask','kind','strength','ev','prompt'] as const)if(JSON.stringify(old[key])!==JSON.stringify(next[key]))throw new Error('SEMANTIC_ATTEMPT_CONFLICT');
   for(const key of ['binding','snapshot','body','ticket'] as const)if(old[key]!==null&&JSON.stringify(old[key])!==JSON.stringify(next[key]))throw new Error('SEMANTIC_ATTEMPT_CONFLICT');
  }
  await this.storage.setItem(this.key,raw);this.current();
 })}
 clear(key:string){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.key);this.current();if(raw!==null&&decode(raw,this.version,this.collection).proposalKey!==key)throw new Error('SEMANTIC_ATTEMPT_CONFLICT');await this.storage.removeItem(this.key)})}
}
export function clearSemanticAttempts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string,lock:SemanticJournalLock=semanticJournalLock){
 epochs.set(origin,(epochs.get(origin)??0)+1);
 return queue(()=>lock(origin,async()=>{await storage.setItem(generationKey(origin),crypto.randomUUID());for(const key of await storage.getAllKeys()){if(!key.startsWith(prefix))continue;let scope:unknown;try{scope=JSON.parse(key.slice(prefix.length))}catch{continue}if(Array.isArray(scope)&&scope[0]===origin)await storage.removeItem(key)}}));
}
