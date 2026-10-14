import {canonicalJSON} from '@photoeditor/api-client';
import {freezeInterchangeCreate,readInterchangeCreateTicket,type InterchangeCreateRequest} from './interchange-create';
import type {InterchangeCreateAttempt,InterchangeCreateAttemptStore} from './interchange-create-attempt-store';
import {semanticJournalLock,type SemanticJournalLock} from './semantic-attempt-store';
const executions=new Map<string,Promise<unknown>>();
async function exclusive<T>(key:string,lock:SemanticJournalLock,action:()=>Promise<T>):Promise<T>{
 const previous=executions.get(key)??Promise.resolve();
 const next=previous.catch(()=>{}).then(()=>lock('interchange-create-execution:'+key,action));
 executions.set(key,next);
 try{return await next;}finally{if(executions.get(key)===next)executions.delete(key);}
}
function own(record:InterchangeCreateAttempt):InterchangeCreateAttempt{
 return {version:1,key:record.key,body:freezeInterchangeCreate(record.body,record.key),ticket:record.ticket?readInterchangeCreateTicket(record.ticket):null};
}
export async function executeInterchangeCreate(options:{api:{createInterchange(body:InterchangeCreateRequest,key:string,signal:AbortSignal):Promise<unknown>};journal:InterchangeCreateAttemptStore;tenant:string;request:InterchangeCreateRequest;signal:AbortSignal;expectedAttemptKey?:string;newKey:()=>string;onAttempt?:(attempt:InterchangeCreateAttempt)=>void;lock?:SemanticJournalLock}):Promise<InterchangeCreateAttempt>{
 const {api,journal,tenant,signal,newKey,onAttempt,expectedAttemptKey}=options;
 const lock=options.lock??semanticJournalLock;
 const request=freezeInterchangeCreate(options.request,'create-controller');journal.assertScope(tenant,request);
 return exclusive(journal.key,lock,async()=>{
  signal.throwIfAborted();journal.assertScope(tenant,request);
  let record=await journal.read();signal.throwIfAborted();
  if(expectedAttemptKey!==undefined&&(!record||record.key!==expectedAttemptKey))throw new Error('INTERCHANGE_CREATE_JOURNAL_CONFLICT');
  if(!record){const key=newKey();record={version:1,key,body:freezeInterchangeCreate(request,key),ticket:null};await journal.write(record);signal.throwIfAborted();}
  const expected=canonicalJSON(record);
  const fence=async()=>{signal.throwIfAborted();journal.assertScope(tenant,request);const current=await journal.read();signal.throwIfAborted();if(!current||canonicalJSON(current)!==expected)throw new Error('INTERCHANGE_CREATE_JOURNAL_CONFLICT');};
  await fence();onAttempt?.(own(record));await fence();
  if(record.ticket)return own(record);
  const ticket=readInterchangeCreateTicket(await api.createInterchange(freezeInterchangeCreate(record.body,record.key),record.key,signal));
  await fence();
  const acknowledged:InterchangeCreateAttempt={...record,ticket};
  await journal.write(acknowledged);signal.throwIfAborted();journal.assertScope(tenant,request);
  onAttempt?.(own(acknowledged));signal.throwIfAborted();journal.assertScope(tenant,request);
  const durable=await journal.read();signal.throwIfAborted();
  if(!durable||canonicalJSON(durable)!==canonicalJSON(acknowledged))throw new Error('INTERCHANGE_CREATE_JOURNAL_CONFLICT');
  return own(acknowledged);
 });
}


export async function openInterchangeCreate(options:{journal:InterchangeCreateAttemptStore;tenant:string;request:InterchangeCreateRequest;expected:InterchangeCreateAttempt;signal:AbortSignal;onCreated:(ticket:NonNullable<InterchangeCreateAttempt['ticket']>)=>void;lock?:SemanticJournalLock}):Promise<void>{
 const {journal,tenant,signal,onCreated}=options;
 const request=freezeInterchangeCreate(options.request,'open-controller'),expected=own(options.expected);
 journal.assertScope(tenant,request);
 return exclusive(journal.key,options.lock??semanticJournalLock,async()=>{
  signal.throwIfAborted();journal.assertScope(tenant,request);
  const current=await journal.read();signal.throwIfAborted();
  journal.assertScope(tenant,request);
  if(!current?.ticket||!expected.ticket||canonicalJSON(current)!==canonicalJSON(expected))throw new Error('INTERCHANGE_CREATE_JOURNAL_CONFLICT');
  onCreated(readInterchangeCreateTicket(current.ticket));
 });
}
