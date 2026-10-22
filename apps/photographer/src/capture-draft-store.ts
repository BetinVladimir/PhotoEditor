import type {CaptureShotRequirement} from './capture-types';
export interface CaptureDraft {version:1;revision:string;requirements:CaptureShotRequirement[];saved_at:number;}
export interface DraftStorage {getItem(key:string):Promise<string|null>;setItem(key:string,value:string):Promise<void>;removeItem(key:string):Promise<void>;}
const uuid=/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
export function parseCaptureDraft(raw:string|null,now=Date.now()):CaptureDraft|null {
 if(!raw||raw.length>500000)return null;
 try{
  const v=JSON.parse(raw) as CaptureDraft;
  if(!v||v.version!==1||typeof v.revision!=='string'||!/^(0|[1-9][0-9]{0,18})$/.test(v.revision)||BigInt(v.revision)>9223372036854775807n||!Number.isSafeInteger(v.saved_at)||v.saved_at>now||now-v.saved_at>7*86400000||!Array.isArray(v.requirements)||v.requirements.length>200)return null;
  const ids=new Set<string>();
  for(const r of v.requirements){
   if(!r||typeof r.id!=='string'||!uuid.test(r.id)||ids.has(r.id)||typeof r.label!=='string'||r.label.length>200||!Array.isArray(r.person_ids)||r.person_ids.length>100||new Set(r.person_ids).size!==r.person_ids.length||r.person_ids.some(id=>typeof id!=='string'||!uuid.test(id))||!Number.isInteger(r.min_confirmed_frames)||r.min_confirmed_frames<1||r.min_confirmed_frames>100||!Number.isFinite(r.min_quality)||r.min_quality<0||r.min_quality>1)return null;
   ids.add(r.id);
  }
  return {version:1,revision:v.revision,saved_at:v.saved_at,requirements:v.requirements.map(r=>({id:r.id,label:r.label,person_ids:r.person_ids,min_confirmed_frames:r.min_confirmed_frames,min_quality:r.min_quality}))};
 }catch{return null}
}
// Serialize writes and deletes across mounts so a late write cannot resurrect a discarded draft.
let pending:Promise<unknown>=Promise.resolve();
const epochs=new Map<string,number>();
const keyEpochs=new Map<string,number>();
const sessionEpochs=new Map<string,number>();
const sessionScope=(origin:string,tenant:string,session:string)=>JSON.stringify([origin,tenant,session]);
function enqueue<T>(action:()=>Promise<T>):Promise<T>{const next=pending.catch(()=>{}).then(action);pending=next;return next;}
export function clearCaptureDrafts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string){
 epochs.set(origin,(epochs.get(origin)??0)+1);
 return enqueue(async()=>{
  for(const key of await storage.getAllKeys()){
   if(!key.startsWith('photo.capture-draft.v1:'))continue;
   try{const scope=JSON.parse(key.slice('photo.capture-draft.v1:'.length));if(Array.isArray(scope)&&scope[0]===origin)await storage.removeItem(key)}catch(error){if(error instanceof SyntaxError)continue;throw error}
  }
 });
}
export function clearCaptureSessionDrafts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string,tenant:string,session:string){
 const scopeKey=sessionScope(origin,tenant,session);
 sessionEpochs.set(scopeKey,(sessionEpochs.get(scopeKey)??0)+1);
 return enqueue(async()=>{
  for(const key of await storage.getAllKeys()){
   if(!key.startsWith('photo.capture-draft.v1:'))continue;
   let scope:unknown;try{scope=JSON.parse(key.slice('photo.capture-draft.v1:'.length))}catch{continue}
   if(Array.isArray(scope)&&scope[0]===origin&&scope[2]===tenant&&scope[3]===session)await storage.removeItem(key);
  }
 });
}
export class CaptureDraftStore {
 readonly key:string;
 private epoch:number;
 private scopeKey:string;
 private sessionEpoch:number;
 private keyEpoch:number;
 constructor(private storage:DraftStorage,private origin:string,user:string,tenant:string,session:string){this.epoch=epochs.get(origin)??0;this.scopeKey=sessionScope(origin,tenant,session);this.sessionEpoch=sessionEpochs.get(this.scopeKey)??0;this.key='photo.capture-draft.v1:'+JSON.stringify([origin,user,tenant,session]);this.keyEpoch=keyEpochs.get(this.key)??0;}
 private current(){return this.epoch===(epochs.get(this.origin)??0)&&this.sessionEpoch===(sessionEpochs.get(this.scopeKey)??0)&&this.keyEpoch===(keyEpochs.get(this.key)??0);}
 private enqueue<T>(action:()=>Promise<T>):Promise<T>{return enqueue(action);}
 read(){return this.enqueue(async()=>{if(!this.current())return null;const value=parseCaptureDraft(await this.storage.getItem(this.key));if(!this.current())return null;if(!value)await this.storage.removeItem(this.key);return value;});}
 write(value:CaptureDraft){const body=JSON.stringify(value);return this.enqueue(async()=>{if(!this.current())return;await this.storage.setItem(this.key,body);});}
 clear(){if(!this.current())return Promise.resolve();this.keyEpoch=(keyEpochs.get(this.key)??0)+1;keyEpochs.set(this.key,this.keyEpoch);return this.enqueue(async()=>{if(this.current())await this.storage.removeItem(this.key);});}
}
