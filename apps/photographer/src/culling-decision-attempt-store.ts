import type {DraftStorage} from './capture-draft-store';
import {readCullingDecisionIntent,cullingDecisionReceipt,type CullingDecisionIntent} from './culling-selection';
import type {Review} from './catalog-types';
export type CullingDecisionAttempt={version:1;key:string;body:CullingDecisionIntent};
import {semanticJournalLock,type SemanticJournalLock} from './semantic-attempt-store';
const prefix='photo.culling-decision-attempt.v1:',epochs=new Map<string,number>();
let pending:Promise<unknown>=Promise.resolve();
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const generationKey=(origin:string)=>'photo.culling-decision-generation.v1:'+JSON.stringify(origin);
function queue<T>(action:()=>Promise<T>){const result=pending.catch(()=>{}).then(action);pending=result;return result;}
export class CullingDecisionAttemptStore {
 readonly key:string;private epoch:number;private generation:string|null|undefined;
 constructor(private storage:DraftStorage,private origin:string,private user:string,private tenant:string,private reviewID:string,private lock:SemanticJournalLock=semanticJournalLock){
  if(typeof origin!=='string'||![user,tenant,reviewID].every(uuid))throw new Error('CULLING_DECISION_JOURNAL_SCOPE_INVALID');
  this.key=prefix+JSON.stringify([origin,user,tenant,reviewID]);this.epoch=epochs.get(origin)??0;
 }
 private current(){if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('CULLING_DECISION_JOURNAL_EXPIRED');}
 private async fence(){this.current();const generation=await this.storage.getItem(generationKey(this.origin));this.current();if(generation!==null&&!uuid(generation))throw new Error('CULLING_DECISION_JOURNAL_INVALID');if(this.generation===undefined)this.generation=generation;else if(this.generation!==generation)throw new Error('CULLING_DECISION_JOURNAL_EXPIRED');}
 private decode(raw:string){
  if(raw.length>(6<<20)||new TextEncoder().encode(raw).byteLength>(6<<20))throw new Error('CULLING_DECISION_JOURNAL_INVALID');
  const parsed=JSON.parse(raw) as CullingDecisionAttempt;
  if(!parsed||Object.keys(parsed).sort().join(',')!=='body,key,version'||parsed.version!==1)throw new Error('CULLING_DECISION_JOURNAL_INVALID');
  if(!uuid(parsed.key))throw new Error('CULLING_DECISION_JOURNAL_INVALID');
  const body=readCullingDecisionIntent(parsed.body);
  if(body.actor!==this.user||body.tenant!==this.tenant||body.selection.review_id!==this.reviewID)throw new Error('CULLING_DECISION_JOURNAL_SCOPE_INVALID');
  const record:CullingDecisionAttempt={version:1,key:parsed.key,body};
  return record;
 }
 private transaction<T>(action:()=>Promise<T>){return queue(()=>this.lock(this.origin,action));}
 read(){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.key);this.current();return raw===null?null:this.decode(raw);});}
 write(record:CullingDecisionAttempt){
  const next=this.decode(JSON.stringify(record)),raw=JSON.stringify(next);
  return this.transaction(async()=>{
   await this.fence();const previous=await this.storage.getItem(this.key);this.current();
   if(previous!==null){
    const old=this.decode(previous);
    if(old.key!==next.key||JSON.stringify(old.body)!==JSON.stringify(next.body))throw new Error('CULLING_DECISION_JOURNAL_CONFLICT');
   }
   await this.storage.setItem(this.key,raw);this.current();
  });
 }
 /** Only an acknowledged attempt can be discarded to author another operation. */
 clear(acceptedKey:string,receipt:Review){
  const acknowledged=structuredClone(receipt);
  return this.transaction(async()=>{
   await this.fence();const raw=await this.storage.getItem(this.key);this.current();
   if(raw!==null){
    const record=this.decode(raw);
    if(record.key!==acceptedKey)throw new Error('CULLING_DECISION_JOURNAL_CONFLICT');
    try{
     if(cullingDecisionReceipt(acknowledged,record.body)===null)throw new Error('Unacknowledged');
    }catch{throw new Error('CULLING_DECISION_JOURNAL_CONFLICT');}
   }
   await this.storage.removeItem(this.key);this.current();
  });
 }

}
export function clearCullingDecisionAttempts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string,lock:SemanticJournalLock=semanticJournalLock){
 epochs.set(origin,(epochs.get(origin)??0)+1);
 return queue(()=>lock(origin,async()=>{
  await storage.setItem(generationKey(origin),crypto.randomUUID());
  for(const key of await storage.getAllKeys()){
   if(!key.startsWith(prefix))continue;
   let scope:unknown;try{scope=JSON.parse(key.slice(prefix.length));}catch{continue;}
   if(Array.isArray(scope)&&scope[0]===origin)await storage.removeItem(key);
  }
 }));
}
