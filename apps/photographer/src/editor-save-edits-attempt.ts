import type {DraftStorage} from './capture-draft-store';
import {semanticJournalLock,type SemanticJournalLock} from './semantic-attempt-store';
import {readCollectionApplyAccepted} from './collection-apply-contract';
import type {ApplyAccepted} from './catalog-types';
import {readDeadline} from './read-deadline';
import {scopeUUID} from './workspace-access';
export type SaveEditsBody={source_revision:number;scope:'selected';selected_item_ids:[string];overrides_id:string;output_name:string;style_version_id?:string};
export type SaveEditsResolution={status:'abandoned';key:string;collection_id:string}|ApplyAccepted;
export function readSaveEditsResolution(raw:unknown,collection:string,key:string):SaveEditsResolution{if(fields(raw,['status','key','collection_id'])&&raw.status==='abandoned'&&raw.key===key&&raw.collection_id===collection)return {status:'abandoned',key,collection_id:collection};return readCollectionApplyAccepted(raw);}
export type SaveEditsAttempt={version:1;key:string;body:SaveEditsBody;accepted:ApplyAccepted|null};
const scopePrefix='photo.editor-save-edits-scope.v1:',generationKey=(origin:string)=>'photo.editor-save-edits-generation.v1:'+JSON.stringify(origin);
const prefix='photo.editor-save-edits.v1:',epochs=new Map<string,number>();let pending:Promise<unknown>=Promise.resolve();
const queue=<T,>(action:()=>Promise<T>)=>{const result=pending.catch(()=>{}).then(action);pending=result;return result;};
const fields=(v:unknown,allowed:string[],required=allowed):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v)&&required.every(k=>Object.hasOwn(v,k))&&Object.keys(v).every(k=>allowed.includes(k));
export function freezeSaveEditsBody(raw:unknown,item:string):SaveEditsBody{
 if(!fields(raw,['source_revision','scope','selected_item_ids','overrides_id','output_name','style_version_id'],['source_revision','scope','selected_item_ids','overrides_id','output_name'])||!Number.isSafeInteger(raw.source_revision)||Number(raw.source_revision)<1||raw.scope!=='selected'||!Array.isArray(raw.selected_item_ids)||raw.selected_item_ids.length!==1||raw.selected_item_ids[0]!==item||!scopeUUID(item)||!scopeUUID(raw.overrides_id)||raw.style_version_id!==undefined&&!scopeUUID(raw.style_version_id)||typeof raw.output_name!=='string'||!raw.output_name.trim()||raw.output_name!==raw.output_name.trim()||Array.from(raw.output_name).length>120)throw Error('SAVE_EDITS_INTENT_INVALID');
 return structuredClone(raw) as SaveEditsBody;
}
export class EditorSaveEditsAttemptStore{
 readonly key:string;private scopeKey:string;private generation:string|null|undefined;private scopeGeneration:string|null|undefined;private acknowledged:string|null=null;private closedOwnKey:string|null=null;private epoch:number;private known:SaveEditsAttempt|null=null;
 constructor(private storage:DraftStorage,private origin:string,actor:string,tenant:string,version:string,private collection:string,private item:string,private lock:SemanticJournalLock=semanticJournalLock){if(!origin||![actor,tenant,version,collection,item].every(scopeUUID))throw Error('SAVE_EDITS_SCOPE_INVALID');const scope=JSON.stringify([origin,actor,tenant,version,collection,item]);this.key=prefix+scope;this.scopeKey=scopePrefix+scope;this.epoch=epochs.get(origin)??0;}
 private fence(signal?:AbortSignal){signal?.throwIfAborted();if(this.epoch!==(epochs.get(this.origin)??0))throw Error('SAVE_EDITS_SCOPE_EXPIRED');}
 private async persistentFence(signal?:AbortSignal){
  this.fence(signal);const generation=await this.storage.getItem(generationKey(this.origin));this.fence(signal);
  if(generation!==null&&!scopeUUID(generation))throw Error('SAVE_EDITS_INTENT_INVALID');
  if(this.generation===undefined)this.generation=generation;else if(this.generation!==generation)throw Error('SAVE_EDITS_SCOPE_EXPIRED');
  const raw=await this.storage.getItem(this.scopeKey);this.fence(signal);let marker:{generation:string;key:string}|null=null;
  if(raw!==null){const value:unknown=JSON.parse(raw);if(raw.length>256||!fields(value,['generation','key'])||!scopeUUID(value.generation)||!scopeUUID(value.key))throw Error('SAVE_EDITS_INTENT_INVALID');marker={generation:value.generation,key:value.key};}
  const current=marker?.generation??null;if(this.scopeGeneration===undefined)this.scopeGeneration=current;else if(this.scopeGeneration!==current)throw Error('SAVE_EDITS_SCOPE_EXPIRED');this.acknowledged=marker?.key??null;
 }
 private async close(record:SaveEditsAttempt,signal:AbortSignal){
  const generation=crypto.randomUUID();await this.storage.setItem(this.scopeKey,JSON.stringify({generation,key:record.key}));
  // A durable closure is authoritative even when its local wait was stopped.
  this.scopeGeneration=generation;this.acknowledged=record.key;this.closedOwnKey=record.key;this.known=null;
  await this.storage.removeItem(this.key);this.fence(signal);
 }
 private decode(raw:string):SaveEditsAttempt{
  if(raw.length>8192)throw Error('SAVE_EDITS_INTENT_INVALID');const value:unknown=JSON.parse(raw);
  if(!fields(value,['version','key','body','accepted'])||value.version!==1||!scopeUUID(value.key))throw Error('SAVE_EDITS_INTENT_INVALID');
  return {version:1,key:value.key,body:freezeSaveEditsBody(value.body,this.item),accepted:value.accepted===null?null:readCollectionApplyAccepted(value.accepted)};
 }
 private identity(a:SaveEditsAttempt,b:SaveEditsAttempt){return a.key===b.key&&JSON.stringify(a.body)===JSON.stringify(b.body);}
 private async load(signal?:AbortSignal){await this.persistentFence(signal);const raw=await this.storage.getItem(this.key);this.fence(signal);const decoded=raw===null?null:this.decode(raw),saved=decoded?.key===this.acknowledged?null:decoded;
  if(this.known?.key===this.acknowledged)this.known=null;
  if(saved&&this.known&&!this.identity(saved,this.known))throw Error('SAVE_EDITS_INTENT_CONFLICT');
  if(saved?.accepted&&this.known?.accepted&&JSON.stringify(saved.accepted)!==JSON.stringify(this.known.accepted))throw Error('SAVE_EDITS_INTENT_CONFLICT');
  const record=this.known?.accepted?this.known:saved??this.known;this.known=record;return record&&structuredClone(record);
 }
 private transaction<T>(action:()=>Promise<T>){return queue(()=>this.lock(this.origin,action));}
 read(signal?:AbortSignal){return this.transaction(()=>this.load(signal));}
 identityKey(){return this.known?.key??null;}
 pending(){return !!this.known&&!this.known.accepted;}
 accepted(){return this.known?.accepted?structuredClone(this.known.accepted):null;}
 submit(body:SaveEditsBody|null,send:(body:SaveEditsBody,key:string,signal:AbortSignal)=>Promise<ApplyAccepted>,signal:AbortSignal){return this.transaction(async()=>{
  let record=await this.load(signal);
  if(!record){if(!body)throw Error('SAVE_EDITS_INTENT_REQUIRED');record={version:1,key:crypto.randomUUID(),body:freezeSaveEditsBody(body,this.item),accepted:null};this.known=record;await this.storage.setItem(this.key,JSON.stringify(record));this.fence(signal);}
  if(record.accepted)return structuredClone(record.accepted);
  // A failed initial write retains the same owned keys; do not POST until the
  // complete frozen command is durably present (including after reload).
  await this.storage.setItem(this.key,JSON.stringify(record));this.fence(signal);
  const accepted=readCollectionApplyAccepted(await readDeadline(signal,child=>send(structuredClone(record!.body),record!.key,child),15000));this.fence(signal);
  record={...record,accepted};this.known=record; // known success precedes persistence
  await this.storage.setItem(this.key,JSON.stringify(record));this.fence(signal);return structuredClone(accepted);
 });}
 resolve(expectedKey:string,send:(body:SaveEditsBody,key:string,signal:AbortSignal)=>Promise<SaveEditsResolution>,signal:AbortSignal){return this.transaction(async()=>{const record=await this.load(signal);if(!record)throw Error('SAVE_EDITS_INTENT_REQUIRED');if(record.key!==expectedKey)throw Error('SAVE_EDITS_INTENT_CONFLICT');if(record.accepted)return structuredClone(record.accepted);const result=readSaveEditsResolution(await send(structuredClone(record.body),record.key,signal),this.collection,record.key);this.fence(signal);if('key' in result){if(result.key!==record.key)throw Error('SAVE_EDITS_INTENT_CONFLICT');await this.close(record,signal);return null;}const accepted=readCollectionApplyAccepted(result);this.known={...record,accepted};await this.storage.setItem(this.key,JSON.stringify(this.known));this.fence(signal);return accepted;});}
 acknowledge(expectedKey:string,signal:AbortSignal){return this.transaction(async()=>{const record=await this.load(signal);if(!record&&this.closedOwnKey===expectedKey)return;if(record?.key!==expectedKey)throw Error('SAVE_EDITS_INTENT_CONFLICT');if(!record?.accepted)throw Error('SAVE_EDITS_RECEIPT_REQUIRED');await this.close(record,signal);});}
}
export function clearEditorSaveEditsAttempts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string,lock:SemanticJournalLock=semanticJournalLock){epochs.set(origin,(epochs.get(origin)??0)+1);return queue(()=>lock(origin,async()=>{await storage.setItem(generationKey(origin),crypto.randomUUID());for(const key of await storage.getAllKeys()){const matched=key.startsWith(prefix)?prefix:key.startsWith(scopePrefix)?scopePrefix:null;if(!matched)continue;let scope:unknown;try{scope=JSON.parse(key.slice(matched.length))}catch{continue}if(Array.isArray(scope)&&scope[0]===origin)await storage.removeItem(key);}}));}
