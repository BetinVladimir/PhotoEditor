import type {DraftStorage} from './capture-draft-store';
import {decodeSemanticApplyRequest,decodeSemanticApplyResult,type SemanticApplyRequest,type SemanticApplyResult} from '@photoeditor/api-client';
export type SemanticApplyAttempt={version:1;key:string;body:SemanticApplyRequest;workflow:string};
import {semanticJournalLock,type SemanticJournalLock} from './semantic-attempt-store';
const scopePrefix='photo.semantic-apply-scope-generation.v1:';
const prefix='photo.semantic-apply-attempt.v1:',epochs=new Map<string,number>();
let pending:Promise<unknown>=Promise.resolve();
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const generationKey=(origin:string)=>'photo.semantic-apply-generation.v1:'+JSON.stringify(origin);
function queue<T>(action:()=>Promise<T>){const result=pending.catch(()=>{}).then(action);pending=result;return result;}
export class SemanticApplyAttemptStore {
 readonly key:string;private scopeKey:string;private scopeGeneration:string|null|undefined;private acknowledgedKey:string|null=null;private operationEpoch=0;private epoch:number;private generation:string|null|undefined;
 constructor(private storage:DraftStorage,private origin:string,user:string,private tenant:string,private intent:string,private lock:SemanticJournalLock=semanticJournalLock){
  if(!origin||![user,tenant,intent].every(uuid))throw new Error('SEMANTIC_APPLY_JOURNAL_SCOPE_INVALID');
  this.key=prefix+JSON.stringify([origin,user,tenant,intent]);this.scopeKey=scopePrefix+JSON.stringify([origin,user,tenant,intent]);this.epoch=epochs.get(origin)??0;
 }
 assertScope(tenant:string,intent:string){this.current();if(tenant!==this.tenant||intent!==this.intent)throw new Error('SEMANTIC_APPLY_JOURNAL_SCOPE_INVALID');}
 private current(){if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('SEMANTIC_APPLY_JOURNAL_EXPIRED');}
 private async fence(){this.current();const generation=await this.storage.getItem(generationKey(this.origin));this.current();if(generation!==null&&!uuid(generation))throw new Error('SEMANTIC_APPLY_JOURNAL_INVALID');if(this.generation===undefined)this.generation=generation;else if(this.generation!==generation)throw new Error('SEMANTIC_APPLY_JOURNAL_EXPIRED');}
 private async scopeFence(){
  const raw=await this.storage.getItem(this.scopeKey);this.current();
  let marker:{generation:string;acknowledged_key:string}|null=null;
  if(raw!==null){
   if(raw.length>256)throw new Error('SEMANTIC_APPLY_JOURNAL_INVALID');
   marker=JSON.parse(raw);
   if(!marker||Object.keys(marker).sort().join(',')!=='acknowledged_key,generation'||!uuid(marker.generation)||!uuid(marker.acknowledged_key))throw new Error('SEMANTIC_APPLY_JOURNAL_INVALID');
  }
  const generation=marker?.generation??null;
  if(this.scopeGeneration===undefined)this.scopeGeneration=generation;
  else if(this.scopeGeneration!==generation)throw new Error('SEMANTIC_APPLY_JOURNAL_EXPIRED');
  this.acknowledgedKey=marker?.acknowledged_key??null;
 }
 private decode(raw:string){
  if(raw.length>(1<<20)||new TextEncoder().encode(raw).byteLength>(1<<20))throw new Error('SEMANTIC_APPLY_JOURNAL_INVALID');
  const parsed=JSON.parse(raw) as SemanticApplyAttempt;
  if(!parsed||Object.keys(parsed).sort().join(',')!=='body,key,version,workflow'||parsed.version!==1)throw new Error('SEMANTIC_APPLY_JOURNAL_INVALID');
  if(!uuid(parsed.key)||!uuid(parsed.workflow))throw new Error('SEMANTIC_APPLY_JOURNAL_INVALID');
  const body=decodeSemanticApplyRequest(parsed.body);
  const record:SemanticApplyAttempt={version:1,key:parsed.key,body,workflow:parsed.workflow};
  return record;
 }
 private transaction<T>(action:()=>Promise<T>){return queue(()=>this.lock(this.origin,action));}
 read(){return this.transaction(async()=>{await this.fence();await this.scopeFence();const raw=await this.storage.getItem(this.key);this.current();if(raw===null)return null;const record=this.decode(raw);return record.key===this.acknowledgedKey?null:record;});}
 write(record:SemanticApplyAttempt){
  const next=this.decode(JSON.stringify(record)),raw=JSON.stringify(next),operationEpoch=this.operationEpoch;
  return this.transaction(async()=>{
   await this.fence();await this.scopeFence();if(operationEpoch!==this.operationEpoch||next.key===this.acknowledgedKey)throw new Error('SEMANTIC_APPLY_JOURNAL_EXPIRED');const previous=await this.storage.getItem(this.key);this.current();
   if(previous!==null){
    const old=this.decode(previous);
    if(old.key!==this.acknowledgedKey&&(old.key!==next.key||old.workflow!==next.workflow||JSON.stringify(old.body)!==JSON.stringify(next.body)))throw new Error('SEMANTIC_APPLY_JOURNAL_CONFLICT');
   }
   await this.storage.setItem(this.key,raw);this.current();
  });
 }
 /** Only an acknowledged attempt can be discarded to author another operation. */
 clear(acceptedKey:string,receipt:SemanticApplyResult){
  const acknowledged=structuredClone(receipt);
  return this.transaction(async()=>{
   await this.fence();await this.scopeFence();const raw=await this.storage.getItem(this.key);this.current();
   if(raw!==null){
    const record=this.decode(raw);
    if(record.key!==acceptedKey)throw new Error('SEMANTIC_APPLY_JOURNAL_CONFLICT');
    try{
     const result=decodeSemanticApplyResult(acknowledged);
     if(result.workflow_id!==record.workflow||result.revision!==(BigInt(record.body.expected_revision)+1n).toString()||
      (result.child!==null)!==record.body.target_decisions.some(item=>item.decision==='approve'))throw new Error('receipt');
    }catch{throw new Error('SEMANTIC_APPLY_JOURNAL_CONFLICT');}
   }
   if(raw!==null&&acceptedKey!==this.acknowledgedKey){
    const generation=crypto.randomUUID();
    await this.storage.setItem(this.scopeKey,JSON.stringify({generation,acknowledged_key:acceptedKey}));this.current();
    this.scopeGeneration=generation;this.acknowledgedKey=acceptedKey;this.operationEpoch++;
   }
   await this.storage.removeItem(this.key);this.current();
  });
 }

}
export function clearSemanticApplyAttempts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string,lock:SemanticJournalLock=semanticJournalLock){
 epochs.set(origin,(epochs.get(origin)??0)+1);
 return queue(()=>lock(origin,async()=>{
  await storage.setItem(generationKey(origin),crypto.randomUUID());
  for(const key of await storage.getAllKeys()){
   const matchedPrefix=key.startsWith(prefix)?prefix:key.startsWith(scopePrefix)?scopePrefix:null;if(!matchedPrefix)continue;
   let scope:unknown;try{scope=JSON.parse(key.slice(matchedPrefix.length));}catch{continue;}
   if(Array.isArray(scope)&&scope[0]===origin)await storage.removeItem(key);
  }
 }));
}
