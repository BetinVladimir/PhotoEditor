import {semanticJournalLock,type SemanticJournalLock} from './semantic-attempt-store';
import type {DraftStorage} from './capture-draft-store';
import {sessionName,editSessionPolicy} from './session-policy';
import type {ShootSessionPolicy,ShootSessionUpdate} from './capture-types';
export type SessionPolicyLocal={name:string;gap:string;neighbors:string;protect:boolean;offsets:Array<{camera_hash:string;seconds:string}>;cameraId:string;cameraSeconds:string};
export type SessionPolicyDraft={schema_version:1;session_id:string;base_revision:string;name:string;policy:ShootSessionPolicy;local?:SessionPolicyLocal;uncertain?:boolean;attempt?:{key:string;request:ShootSessionUpdate};saved_at:number};
const prefix='photo.session-policy-draft.v1:',uuid=/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const validID=(id:unknown):id is string=>typeof id==='string'&&uuid.test(id)&&id!=='00000000-0000-0000-0000-000000000000';
const revokedScopes=new Map<string,number>();
const scopeKey=(origin:string,tenant:string,session:string)=>JSON.stringify([origin,tenant,session]);
const generationKey=(origin:string)=>'photo.session-policy-generation.v1:'+JSON.stringify(origin);
const scopeGenerationKey=(scope:string)=>'photo.session-policy-scope-generation.v1:'+scope;
const epochs=new Map<string,number>();let pending:Promise<unknown>=Promise.resolve();
function enqueue<T>(action:()=>Promise<T>){const next=pending.catch(()=>{}).then(action);pending=next;return next;}
export function parseSessionPolicyDraft(raw:string|null,session:string,now=Date.now()):SessionPolicyDraft|null{
 if(!raw||raw.length>16384||!validID(session))return null;
 try{const v=JSON.parse(raw) as SessionPolicyDraft,p=v.policy;
  if(!v||Object.keys(v).filter(key=>key!=='local'&&key!=='uncertain'&&key!=='attempt').sort().join(',')!=='base_revision,name,policy,saved_at,schema_version,session_id'||v.schema_version!==1||v.session_id!==session||typeof v.base_revision!=='string'||!/^[1-9][0-9]{0,18}$/.test(v.base_revision)||BigInt(v.base_revision)>9223372036854775807n||!Number.isSafeInteger(v.saved_at)||v.saved_at<0||v.saved_at>now||now-v.saved_at>7*86400000||!p||Object.keys(p).sort().join(',')!=='calibration,camera_offsets,neighbor_count,protect_manual_picks,schema_version,time_gap_seconds'||p.schema_version!==1||typeof p.protect_manual_picks!=='boolean'||!Number.isInteger(p.time_gap_seconds)||!Number.isInteger(p.neighbor_count)||!Array.isArray(p.camera_offsets)||p.camera_offsets.length>32||!p.calibration||Object.keys(p.calibration).sort().join(',')!=='artifact_id,sha256'||!validID(p.calibration.artifact_id)||typeof p.calibration.sha256!=='string'||!/^[0-9a-f]{64}$/.test(p.calibration.sha256))return null;
  const offsets=p.camera_offsets.map(value=>{if(!value||Object.keys(value).sort().join(',')!=='camera_hash,seconds'||!Number.isSafeInteger(value.seconds))throw new Error('INVALID_OFFSET');return {camera_hash:value.camera_hash,seconds:String(value.seconds)};});
  if(v.uncertain!==undefined&&typeof v.uncertain!=='boolean')return null;
  const attempt=v.attempt===undefined?undefined:parseSessionPolicyAttempt(v.attempt,session,now);if(v.attempt!==undefined&&(!attempt||v.uncertain!==true))return null;
  const local=v.local===undefined?undefined:parseSessionPolicyLocal(v.local);if(v.local!==undefined&&!local)return null;
  return {schema_version:1,session_id:session,...(attempt?{attempt}:{}),...(v.uncertain===undefined?{}:{uncertain:v.uncertain}),...(local?{local}:{}),base_revision:v.base_revision,name:sessionName(v.name),policy:editSessionPolicy(p,String(p.time_gap_seconds),String(p.neighbor_count),p.protect_manual_picks,offsets),saved_at:v.saved_at};
 }catch{return null;}
}
/** Epoch changes before queueing deletes: pending writes cannot resurrect logged-out data. */
export function clearSessionPolicyDrafts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string,lock:SemanticJournalLock=semanticJournalLock){
 epochs.set(origin,(epochs.get(origin)??0)+1);
 return enqueue(()=>lock(origin,async()=>{await storage.setItem(generationKey(origin),crypto.randomUUID());for(const key of await storage.getAllKeys()){if(!key.startsWith(prefix))continue;let scope:unknown;try{scope=JSON.parse(key.slice(prefix.length));}catch{continue;}if(Array.isArray(scope)&&scope[0]===origin)await storage.removeItem(key);}}));
}
export class SessionPolicyDraftStore{
 readonly key:string;private epoch:number;private scope:string;private scopeEpoch:number;private expired=false;private generation:string|null|undefined;private scopeGeneration:string|null|undefined;
 constructor(private storage:DraftStorage,private origin:string,user:string,tenant:string,private session:string,private lock:SemanticJournalLock=semanticJournalLock){if(!origin||![user,tenant,session].every(validID))throw new Error('SESSION_DRAFT_SCOPE_INVALID');this.key=prefix+JSON.stringify([origin,user,tenant,session]);this.epoch=epochs.get(origin)??0;this.scope=scopeKey(origin,tenant,session);this.scopeEpoch=revokedScopes.get(this.scope)??0;}
 private transaction<T>(action:()=>Promise<T>){return enqueue(()=>this.lock(this.origin,async()=>{
  const generation=await this.storage.getItem(generationKey(this.origin)),scopeGeneration=await this.storage.getItem(scopeGenerationKey(this.scope));
  if(generation!==null&&!validID(generation)||scopeGeneration!==null&&!validID(scopeGeneration))throw new Error('SESSION_DRAFT_GENERATION_INVALID');
  if(this.generation===undefined){this.generation=generation;this.scopeGeneration=scopeGeneration;}
  else if(this.generation!==generation||this.scopeGeneration!==scopeGeneration)this.expired=true;
  return action();
 }));}
 private valid(){return !this.expired&&this.epoch===(epochs.get(this.origin)??0)&&this.scopeEpoch===(revokedScopes.get(this.scope)??0);}
 read(){return this.transaction(async()=>{if(!this.valid())return null;const value=parseSessionPolicyDraft(await this.storage.getItem(this.key),this.session);if(!this.valid())return null;if(!value)await this.storage.removeItem(this.key);return value;});}
 write(value:SessionPolicyDraft,expectedAttemptKey?:string){const parsed=parseSessionPolicyDraft(JSON.stringify(value),this.session);if(!parsed)return Promise.reject(new Error('SESSION_DRAFT_INVALID'));return this.transaction(async()=>{
  if(!this.valid())throw new Error('SESSION_DRAFT_SCOPE_EXPIRED');
  const current=parseSessionPolicyDraft(await this.storage.getItem(this.key),this.session);
  if(!this.valid())throw new Error('SESSION_DRAFT_SCOPE_EXPIRED');
  if(expectedAttemptKey!==undefined){if(current?.attempt?.key!==expectedAttemptKey)throw new Error('SESSION_ATTEMPT_CHANGED');}
  else if(current?.attempt&&(!parsed.attempt||current.attempt.key!==parsed.attempt.key||JSON.stringify(current.attempt.request)!==JSON.stringify(parsed.attempt.request)))throw new Error('SESSION_ATTEMPT_CHANGED');
  await this.storage.setItem(this.key,JSON.stringify(parsed));
 });}
 /** Reserve lifecycle request atomically within the existing storage queue. */
 reserveLifecycle(value:SessionPolicyDraft){const parsed=parseSessionPolicyDraft(JSON.stringify(value),this.session);if(!parsed?.attempt)return Promise.reject(new Error('SESSION_LIFECYCLE_ATTEMPT_INVALID'));return this.transaction(async()=>{
  if(!this.valid())throw new Error('SESSION_DRAFT_SCOPE_EXPIRED');
  const raw=await this.storage.getItem(this.key);if(!this.valid())throw new Error('SESSION_DRAFT_SCOPE_EXPIRED');
  const current=parseSessionPolicyDraft(raw,this.session);
  if(raw!==null&&!current)throw new Error('SESSION_DRAFT_INVALID');
  if(current){if(!current.attempt)throw new Error('SESSION_LIFECYCLE_DRAFT_CONFLICT');if(current.attempt.key!==parsed.attempt!.key||JSON.stringify(current.attempt.request)!==JSON.stringify(parsed.attempt!.request))throw new Error('SESSION_LIFECYCLE_ATTEMPT_CONFLICT');return;}
  await this.storage.setItem(this.key,JSON.stringify(parsed));if(!this.valid())throw new Error('SESSION_DRAFT_SCOPE_EXPIRED');
 });}
 clearAttempt(key:string){return this.transaction(async()=>{if(!this.valid())return false;const current=parseSessionPolicyDraft(await this.storage.getItem(this.key),this.session);if(!this.valid()||current?.attempt?.key!==key)return false;await this.storage.removeItem(this.key);return true;});}
 clear(expectedAttemptKey?:string){return this.transaction(async()=>{if(!this.valid())throw new Error('SESSION_DRAFT_SCOPE_EXPIRED');const current=parseSessionPolicyDraft(await this.storage.getItem(this.key),this.session);if(expectedAttemptKey!==undefined?current?.attempt?.key!==expectedAttemptKey:!!current?.attempt)throw new Error('SESSION_ATTEMPT_CHANGED');await this.storage.removeItem(this.key);});}
}

/** Bounded raw fields: invalid partial input is a draft, never an admitted policy. */
export function parseSessionPolicyLocal(value:unknown):SessionPolicyLocal|null{
 if(!value||typeof value!=='object'||Array.isArray(value))return null;
 const v=value as SessionPolicyLocal;
 if(Object.keys(v).sort().join(',')!=='cameraId,cameraSeconds,gap,name,neighbors,offsets,protect'||typeof v.protect!=='boolean'||!Array.isArray(v.offsets)||v.offsets.length>32)return null;
 for(const [field,limit] of [['name',400],['gap',4],['neighbors',2],['cameraId',64],['cameraSeconds',6]] as const)if(typeof v[field]!=='string'||v[field].length>limit)return null;
 const ids=new Set<string>();const offsets=[];
 for(const offset of v.offsets){if(!offset||Object.keys(offset).sort().join(',')!=='camera_hash,seconds'||typeof offset.camera_hash!=='string'||!/^[0-9a-f]{64}$/.test(offset.camera_hash)||ids.has(offset.camera_hash)||typeof offset.seconds!=='string'||offset.seconds.length>6)return null;ids.add(offset.camera_hash);offsets.push({...offset});}
 return {name:v.name,gap:v.gap,neighbors:v.neighbors,protect:v.protect,offsets,cameraId:v.cameraId,cameraSeconds:v.cameraSeconds};
}

/** Access denial invalidates in-flight stores before serialized scoped deletion. */
export function revokeSessionPolicyDrafts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string,tenant:string,session:string,lock:SemanticJournalLock=semanticJournalLock){
 const scope=scopeKey(origin,tenant,session);revokedScopes.set(scope,(revokedScopes.get(scope)??0)+1);
 return enqueue(()=>lock(origin,async()=>{await storage.setItem(scopeGenerationKey(scope),crypto.randomUUID());for(const key of await storage.getAllKeys()){if(!key.startsWith(prefix))continue;let parts:unknown;try{parts=JSON.parse(key.slice(prefix.length));}catch{continue;}if(Array.isArray(parts)&&parts.length===4&&parts[0]===origin&&parts[2]===tenant&&parts[3]===session)await storage.removeItem(key);}}));
}

export function parseSessionPolicyAttempt(value:unknown,session:string,now=Date.now()):SessionPolicyDraft['attempt']|null{
 try{const a=value as NonNullable<SessionPolicyDraft['attempt']>,r=a.request;
 if(!a||Object.keys(a).sort().join(',')!=='key,request'||!validID(a.key)||!r||Object.keys(r).sort().join(',')!=='capture_policy,expected_revision,name,policy,state'||!['open','closed','archived'].includes(r.state))return null;
 const base=parseSessionPolicyDraft(JSON.stringify({schema_version:1,session_id:session,base_revision:r.expected_revision,name:r.name,policy:r.policy,saved_at:now}),session,now);if(!base)return null;
 const c=r.capture_policy;if(c!==null){if(!c||Object.keys(c).sort().join(',')!=='calibration,checks,max_queue_age_seconds,schema_version'||c.schema_version!==1||c.max_queue_age_seconds!==30||!Array.isArray(c.checks)||c.checks.length<1||c.checks.length>6||new Set(c.checks).size!==c.checks.length||c.checks.some(k=>!['focus','eyes','clipping','glare','duplicate','shot_list'].includes(k))||!c.calibration||Object.keys(c.calibration).sort().join(',')!=='artifact_id,sha256'||!validID(c.calibration.artifact_id)||typeof c.calibration.sha256!=='string'||!/^[0-9a-f]{64}$/.test(c.calibration.sha256))return null;}
 return {key:a.key,request:{expected_revision:base.base_revision,name:base.name,state:r.state,policy:base.policy,capture_policy:structuredClone(c)}};
 }catch{return null;}
}
