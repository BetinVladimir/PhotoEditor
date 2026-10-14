import {canonicalJSON} from '@photoeditor/api-client';
import type {DraftStorage} from './capture-draft-store';
import type {SceneAnalysisRequest} from './scene-analysis-request';
import {parseSessionPolicyDraft} from './session-policy-draft';
import {semanticJournalLock,type SemanticJournalLock} from './semantic-attempt-store';

export type SceneReferenceAttempt={schema_version:1;session_id:string;analysis_id:string;key:string;request:SceneAnalysisRequest;ticket:{run_id:string}|null};
const prefix='photo.scene-reference-attempt.v1:',epochs=new Map<string,number>();
const generationKey=(origin:string)=>'photo.scene-reference-generation.v1:'+JSON.stringify(origin);
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
let pending:Promise<unknown>=Promise.resolve();
function queue<T>(action:()=>Promise<T>){const next=pending.catch(()=>{}).then(action);pending=next;return next;}
function fields(v:unknown,keys:string[]){return !!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).sort().join(',')===keys.sort().join(',');}
function freezeRequest(value:SceneAnalysisRequest,session:string,analysis:string):SceneAnalysisRequest{
 if(!fields(value,['expected_revision','selection_snapshot_id','policy','manual_references'])||typeof value.expected_revision!=='string'||!/^[1-9][0-9]{0,18}$/.test(value.expected_revision)||BigInt(value.expected_revision)>9223372036854775807n||!uuid(value.selection_snapshot_id)||!Array.isArray(value.manual_references)||value.manual_references.length<1||value.manual_references.length>5000)throw new Error('SCENE_REFERENCE_JOURNAL_INVALID');
 const checked=parseSessionPolicyDraft(JSON.stringify({schema_version:1,session_id:session,base_revision:value.expected_revision,name:'Scene',policy:value.policy,saved_at:Date.now()}),session);
 if(!checked)throw new Error('SCENE_REFERENCE_JOURNAL_INVALID');
 const groups=new Set<string>();for(const ref of value.manual_references){if(!fields(ref,['previous_analysis_id','group_id','asset_version_id'])||ref.previous_analysis_id!==analysis||!uuid(ref.group_id)||!uuid(ref.asset_version_id)||groups.has(ref.group_id))throw new Error('SCENE_REFERENCE_JOURNAL_INVALID');groups.add(ref.group_id);}
 const owned={expected_revision:value.expected_revision,selection_snapshot_id:value.selection_snapshot_id,policy:checked.policy,manual_references:value.manual_references.map(ref=>({...ref}))};
 if(new TextEncoder().encode(canonicalJSON(owned)).byteLength>1048576)throw new Error('SCENE_REFERENCE_JOURNAL_INVALID');return owned;
}
export class SceneReferenceAttemptStore {
 readonly storageKey:string;private epoch:number;private generation:string|null|undefined;
 constructor(private storage:DraftStorage,private origin:string,user:string,tenant:string,private session:string,private analysis:string,private lock:SemanticJournalLock=semanticJournalLock){if(!origin||![user,tenant,session,analysis].every(uuid))throw new Error('SCENE_REFERENCE_JOURNAL_SCOPE_INVALID');this.storageKey=prefix+JSON.stringify([origin,user,tenant,session,analysis]);this.epoch=epochs.get(origin)??0;}
 private current(){if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('SCENE_REFERENCE_JOURNAL_EXPIRED');}
 private async fence(){this.current();const generation=await this.storage.getItem(generationKey(this.origin));this.current();if(generation!==null&&!uuid(generation))throw new Error('SCENE_REFERENCE_JOURNAL_INVALID');if(this.generation===undefined)this.generation=generation;else if(this.generation!==generation)throw new Error('SCENE_REFERENCE_JOURNAL_EXPIRED');}
 private decode(raw:string):SceneReferenceAttempt{if(raw.length>1100000)throw new Error('SCENE_REFERENCE_JOURNAL_INVALID');const value=JSON.parse(raw) as SceneReferenceAttempt;if(!fields(value,['schema_version','session_id','analysis_id','key','request','ticket'])||value.schema_version!==1||value.session_id!==this.session||value.analysis_id!==this.analysis||!uuid(value.key)||value.ticket!==null&&(!fields(value.ticket,['run_id'])||!uuid(value.ticket.run_id)))throw new Error('SCENE_REFERENCE_JOURNAL_INVALID');return {schema_version:1,session_id:value.session_id,analysis_id:value.analysis_id,key:value.key,request:freezeRequest(value.request,this.session,this.analysis),ticket:value.ticket===null?null:{run_id:value.ticket.run_id}};}
 private transaction<T>(action:()=>Promise<T>){return queue(()=>this.lock(this.origin,action));}
 read(){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.storageKey);this.current();return raw===null?null:this.decode(raw);});}
 write(value:SceneReferenceAttempt){const next=this.decode(JSON.stringify(value)),raw=JSON.stringify(next);return this.transaction(async()=>{await this.fence();const previous=await this.storage.getItem(this.storageKey);this.current();if(previous!==null){const old=this.decode(previous);if(old.key!==next.key||JSON.stringify(old.request)!==JSON.stringify(next.request)||old.ticket!==null&&JSON.stringify(old.ticket)!==JSON.stringify(next.ticket))throw new Error('SCENE_REFERENCE_JOURNAL_CONFLICT');}await this.storage.setItem(this.storageKey,raw);this.current();});}
 discardRejected(key:string){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.storageKey);this.current();if(raw!==null){const old=this.decode(raw);if(old.key!==key||old.ticket!==null)throw new Error('SCENE_REFERENCE_JOURNAL_CONFLICT');await this.storage.removeItem(this.storageKey);this.current();}});}
}
export function clearSceneReferenceAttempts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string,lock:SemanticJournalLock=semanticJournalLock){epochs.set(origin,(epochs.get(origin)??0)+1);return queue(()=>lock(origin,async()=>{await storage.setItem(generationKey(origin),crypto.randomUUID());for(const key of await storage.getAllKeys()){if(!key.startsWith(prefix))continue;let scope:unknown;try{scope=JSON.parse(key.slice(prefix.length));}catch{continue;}if(Array.isArray(scope)&&scope[0]===origin)await storage.removeItem(key);}}));}
