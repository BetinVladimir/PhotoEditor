import {canonicalJSON} from '@photoeditor/api-client';
import {freezeInterchangeSnapshot,readInterchangeSnapshotReceipt,type InterchangeSnapshotRequest} from './interchange-export-snapshot';
import type {InterchangeSnapshotAttempt,InterchangeSnapshotAttemptStore} from './interchange-snapshot-attempt-store';
import {semanticJournalLock,type SemanticJournalLock} from './semantic-attempt-store';
const executions=new Map<string,Promise<unknown>>();
async function exclusive<T>(key:string,lock:SemanticJournalLock,action:()=>Promise<T>):Promise<T>{
 const previous=executions.get(key)??Promise.resolve();
 const next=previous.catch(()=>{}).then(()=>lock('interchange-snapshot-execution:'+key,action));
 executions.set(key,next);
 try{return await next;}finally{if(executions.get(key)===next)executions.delete(key);}
}
function untilAbort<T>(operation:Promise<T>,signal:AbortSignal):Promise<T>{
 return new Promise<T>((resolve,reject)=>{
  const abort=()=>{signal.removeEventListener('abort',abort);reject(signal.reason??new Error('INTERCHANGE_SNAPSHOT_CANCELLED'));};
  signal.addEventListener('abort',abort,{once:true});
  operation.then(value=>{signal.removeEventListener('abort',abort);if(signal.aborted)abort();else resolve(value);},reason=>{signal.removeEventListener('abort',abort);reject(reason);});
  if(signal.aborted)abort();
 });
}
function own(record:InterchangeSnapshotAttempt):InterchangeSnapshotAttempt{
 return {version:1,key:record.key,body:freezeInterchangeSnapshot(record.body,record.key),receipt:record.receipt?readInterchangeSnapshotReceipt(record.receipt,record.body):null};
}
export async function executeInterchangeSnapshot(options:{api:{prepareInterchangeSnapshot(project:string,body:InterchangeSnapshotRequest,key:string,signal:AbortSignal):Promise<unknown>};journal:InterchangeSnapshotAttemptStore;tenant:string;project:string;request:InterchangeSnapshotRequest;signal:AbortSignal;expectedAttemptKey?:string;newKey:()=>string;onAttempt?:(attempt:InterchangeSnapshotAttempt)=>void;lock?:SemanticJournalLock}):Promise<InterchangeSnapshotAttempt>{
 const {api,journal,tenant,project,signal,newKey,onAttempt,expectedAttemptKey}=options;
 const lock=options.lock??semanticJournalLock;
 const request=freezeInterchangeSnapshot(options.request,'snapshot-controller');journal.assertScope(tenant,project,request);
 return exclusive(journal.key,lock,async()=>{
  signal.throwIfAborted();journal.assertScope(tenant,project,request);
  let record=await journal.read();signal.throwIfAborted();
  if(expectedAttemptKey!==undefined&&(!record||record.key!==expectedAttemptKey))throw new Error('INTERCHANGE_SNAPSHOT_JOURNAL_CONFLICT');
  if(!record){const key=newKey();record={version:1,key,body:freezeInterchangeSnapshot(request,key),receipt:null};await journal.write(record);signal.throwIfAborted();}
  const expected=canonicalJSON(record);
  const fence=async()=>{signal.throwIfAborted();journal.assertScope(tenant,project,request);const current=await journal.read();signal.throwIfAborted();if(!current||canonicalJSON(current)!==expected)throw new Error('INTERCHANGE_SNAPSHOT_JOURNAL_CONFLICT');};
  await fence();onAttempt?.(own(record));await fence();
  if(record.receipt)return own(record);
  const receipt=readInterchangeSnapshotReceipt(await untilAbort(api.prepareInterchangeSnapshot(project,freezeInterchangeSnapshot(record.body,record.key),record.key,signal),signal),record.body);
  await fence();
  const acknowledged:InterchangeSnapshotAttempt={...record,receipt};
  await journal.write(acknowledged);signal.throwIfAborted();journal.assertScope(tenant,project,request);
  onAttempt?.(own(acknowledged));signal.throwIfAborted();journal.assertScope(tenant,project,request);
  const durable=await journal.read();signal.throwIfAborted();
  if(!durable||canonicalJSON(durable)!==canonicalJSON(acknowledged))throw new Error('INTERCHANGE_SNAPSHOT_JOURNAL_CONFLICT');
  return own(acknowledged);
 });
}


export async function openInterchangeSnapshot(options:{journal:InterchangeSnapshotAttemptStore;tenant:string;project:string;request:InterchangeSnapshotRequest;expected:InterchangeSnapshotAttempt;signal:AbortSignal;onPrepared:(receipt:NonNullable<InterchangeSnapshotAttempt['receipt']>)=>void;lock?:SemanticJournalLock}):Promise<void>{
 const {journal,tenant,project,signal,onPrepared}=options;
 const request=freezeInterchangeSnapshot(options.request,'open-controller'),expected=own(options.expected);
 journal.assertScope(tenant,project,request);
 return exclusive(journal.key,options.lock??semanticJournalLock,async()=>{
  signal.throwIfAborted();journal.assertScope(tenant,project,request);
  const current=await journal.read();signal.throwIfAborted();
  journal.assertScope(tenant,project,request);
  if(!current?.receipt||!expected.receipt||canonicalJSON(current)!==canonicalJSON(expected))throw new Error('INTERCHANGE_SNAPSHOT_JOURNAL_CONFLICT');
  onPrepared(readInterchangeSnapshotReceipt(current.receipt,current.body));
 });
}
