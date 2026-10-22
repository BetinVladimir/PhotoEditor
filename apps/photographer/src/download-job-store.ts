import {semanticJournalLock,type SemanticJournalLock} from './semantic-attempt-store';
import {APIError} from "@photoeditor/api-client";
import type {DraftStorage} from './capture-draft-store';
let pending:Promise<unknown>=Promise.resolve();
function queue<T>(action:()=>Promise<T>){const result=pending.catch(()=>{}).then(action);pending=result;return result}
const prefix='photo.download-job.v1:';
const epochs=new Map<string,number>();
const generationKey=(origin:string)=>'photo.download-job-generation.v1:'+JSON.stringify(origin);
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
export type DownloadStartAttempt={key:string;snapshot:string;preset:string};
export class DownloadJobStore{
 private key:string;private attemptKey:string;private epoch:number;private generation:string|null|undefined;
 constructor(private storage:DraftStorage,private origin:string,user:string,tenant:string,private lock:SemanticJournalLock=semanticJournalLock){
  if(!origin||!uuid(user)||!uuid(tenant))throw new Error('DOWNLOAD_STORE_SCOPE_INVALID');
  this.key=prefix+JSON.stringify([origin,user,tenant]);this.attemptKey=prefix+JSON.stringify([origin,user,tenant,"pending"]);this.epoch=epochs.get(origin)??0;
 }
 private current(){if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('DOWNLOAD_STORE_EXPIRED')}
 private async fence(){this.current();const value=await this.storage.getItem(generationKey(this.origin));this.current();if(value!==null&&!uuid(value))throw new Error('DOWNLOAD_STORE_INVALID');if(this.generation===undefined)this.generation=value;else if(this.generation!==value)throw new Error('DOWNLOAD_STORE_EXPIRED')}
 private transaction<T>(action:()=>Promise<T>){return queue(()=>this.lock(this.origin,action))}
 read(){return this.transaction(async()=>{await this.fence();const value=await this.storage.getItem(this.key);this.current();if(value!==null&&!uuid(value))throw new Error('DOWNLOAD_STORE_INVALID');return value})}
 write(id:string){return this.transaction(async()=>{await this.fence();if(!uuid(id))throw new Error('DOWNLOAD_STORE_INVALID');await this.storage.setItem(this.key,id);this.current()})}
 private decodeAttempt(raw:string):DownloadStartAttempt{
  if(raw.length>1024)throw new Error('DOWNLOAD_ATTEMPT_INVALID');const v=JSON.parse(raw) as DownloadStartAttempt;
  if(!v||Object.keys(v).sort().join(',')!=='key,preset,snapshot'||![v.key,v.snapshot,v.preset].every(uuid))throw new Error('DOWNLOAD_ATTEMPT_INVALID');return {...v};
 }
 readAttempt(){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.attemptKey);this.current();return raw===null?null:this.decodeAttempt(raw)})}
 begin(snapshot:string,preset:string){return this.transaction(async()=>{
  await this.fence();const raw=await this.storage.getItem(this.attemptKey);this.current();if(raw!==null)return this.decodeAttempt(raw);
  const value=this.decodeAttempt(JSON.stringify({key:crypto.randomUUID(),snapshot,preset}));await this.storage.setItem(this.attemptKey,JSON.stringify(value));this.current();return value;
 })}
 refuse(attempt:DownloadStartAttempt,error:unknown){return this.transaction(async()=>{
  await this.fence();
  if(!(error instanceof APIError)||!['SNAPSHOT_STALE','VALIDATION_FAILED','PRESET_NOT_ALLOWED'].includes(error.code))return false;
  const raw=await this.storage.getItem(this.attemptKey);this.current();
  if(raw===null||JSON.stringify(this.decodeAttempt(raw))!==JSON.stringify(attempt))throw new Error('DOWNLOAD_ATTEMPT_CONFLICT');
  await this.storage.removeItem(this.attemptKey);this.current();return true;
 })}

 complete(attempt:DownloadStartAttempt,id:string){return this.transaction(async()=>{
  await this.fence();if(!uuid(id))throw new Error('DOWNLOAD_STORE_INVALID');const raw=await this.storage.getItem(this.attemptKey);this.current();
  if(raw===null||JSON.stringify(this.decodeAttempt(raw))!==JSON.stringify(attempt))throw new Error('DOWNLOAD_ATTEMPT_CONFLICT');
  await this.storage.setItem(this.key,id);this.current();await this.storage.removeItem(this.attemptKey);this.current();
 })}

}
export async function clearDownloadJobs(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string,lock:SemanticJournalLock=semanticJournalLock){
 epochs.set(origin,(epochs.get(origin)??0)+1);
 return queue(()=>lock(origin,async()=>{await storage.setItem(generationKey(origin),crypto.randomUUID());for(const key of await storage.getAllKeys())if(key.startsWith(prefix)){
  try{if((JSON.parse(key.slice(prefix.length)) as unknown[])[0]===origin)await storage.removeItem(key)}catch{/* skip unrelated malformed keys */}
 }}))
}
