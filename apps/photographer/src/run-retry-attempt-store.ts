import type {DraftStorage} from './capture-draft-store';

export type RunRetryAttempt={version:1;key:string;expected_revision:string;created_at:number};
const prefix='photo.run-retry-attempt.v1:';
const epochs=new Map<string,number>();
let pending:Promise<unknown>=Promise.resolve();
const uuid=/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const revision=/^[1-9][0-9]{0,18}$/;
function queue<T>(action:()=>Promise<T>):Promise<T>{const result=pending.catch(()=>{}).then(action);pending=result;return result;}

export class RunRetryAttemptStore {
 readonly storageKey:string;
 private epoch:number;
 constructor(private storage:DraftStorage,private origin:string,user:string,tenant:string,run:string){
  if(!origin||![user,tenant,run].every(value=>uuid.test(value)))throw new Error('RUN_RETRY_SCOPE_INVALID');
  this.storageKey=prefix+JSON.stringify([origin,user,tenant,run]);
  this.epoch=epochs.get(origin)??0;
 }
 private current(){if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('RUN_RETRY_ATTEMPT_EXPIRED');}
 private decode(raw:string):RunRetryAttempt{
  if(raw.length>512)throw new Error('RUN_RETRY_ATTEMPT_INVALID');
  const value=JSON.parse(raw) as RunRetryAttempt;
  if(!value||Object.keys(value).sort().join(',')!=='created_at,expected_revision,key,version'||value.version!==1||!uuid.test(value.key)||typeof value.expected_revision!=='string'||!revision.test(value.expected_revision)||BigInt(value.expected_revision)>9223372036854775807n||!Number.isSafeInteger(value.created_at)||value.created_at<0)throw new Error('RUN_RETRY_ATTEMPT_INVALID');
  return value;
 }
 read(){return queue(async()=>{this.current();const raw=await this.storage.getItem(this.storageKey);this.current();return raw===null?null:this.decode(raw);});}
 begin(expectedRevision:string,now=Date.now()){
  return queue(async()=>{
   this.current();const raw=await this.storage.getItem(this.storageKey);this.current();
   if(raw!==null)return this.decode(raw);
   const record=this.decode(JSON.stringify({version:1,key:crypto.randomUUID(),expected_revision:expectedRevision,created_at:now}));
   await this.storage.setItem(this.storageKey,JSON.stringify(record));this.current();return record;
  });
 }
 acknowledge(key:string){return queue(async()=>{this.current();const raw=await this.storage.getItem(this.storageKey);this.current();if(raw===null)return;const record=this.decode(raw);if(record.key!==key)throw new Error('RUN_RETRY_ATTEMPT_CONFLICT');await this.storage.removeItem(this.storageKey);this.current();});}
}

export function clearRunRetryAttempts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string){
 epochs.set(origin,(epochs.get(origin)??0)+1);
 return queue(async()=>{for(const key of await storage.getAllKeys()){if(!key.startsWith(prefix))continue;let scope:unknown;try{scope=JSON.parse(key.slice(prefix.length));}catch{continue;}if(Array.isArray(scope)&&scope[0]===origin)await storage.removeItem(key);}});
}
