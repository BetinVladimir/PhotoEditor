import {assertArchiveHandoffAttempt} from './interchange-archive-handoff';
import {canonicalJSON,type UploadSource} from '@photoeditor/api-client';
import {InterchangeArchiveAttemptStore,type InterchangeArchiveAttempt} from './interchange-archive-attempt-store';
import {executeArchiveUpload,putArchivePart,type ArchiveUploadTransport,type ArchivePartPut} from './interchange-archive-executor';
import {archiveRequestSHA,archiveUploadID,freezeArchiveUpload,readArchiveUploadReceipt,readArchiveAbortAccepted,readArchiveStatus,type ArchiveUploadRequest,type ArchiveUploadReceipt,type ArchiveStatus} from './interchange-archive-upload';
import {semanticJournalLock,type SemanticJournalLock} from './semantic-attempt-store';
export interface ArchiveUploadControllerAPI extends ArchiveUploadTransport{
 beginInterchangeArchive(project:string,request:ArchiveUploadRequest,key:string,signal:AbortSignal):Promise<ArchiveUploadReceipt>;
}
/** Read the server ledger for an intent that may not yet have a Begin receipt. */
export async function checkArchiveAttempt(options:{api:{interchangeArchiveAttemptStatus(user:string,project:string,request:ArchiveUploadRequest,key:string,signal:AbortSignal):Promise<ArchiveStatus>};journal:InterchangeArchiveAttemptStore;user:string;tenant:string;project:string;request:ArchiveUploadRequest;signal:AbortSignal;lock?:SemanticJournalLock}){
 const {api,journal,user,tenant,project,signal}=options,request=freezeArchiveUpload(options.request,'attempt-check');journal.assertScope(tenant,project,request);
 return exclusive(journal.key,async()=>{
  signal.throwIfAborted();const saved=await journal.read();signal.throwIfAborted();
  if(!saved)throw new Error('INTERCHANGE_ARCHIVE_JOURNAL_CONFLICT');
  const expected=canonicalJSON(saved);
  const value=await api.interchangeArchiveAttemptStatus(user,project,request,saved.key,signal);signal.throwIfAborted();
  const expected:ArchiveUploadReceipt={upload_id:archiveUploadID(tenant,user,saved.key),state:'uploading',part_bytes:16777216,parts_total:Math.ceil(request.bytes/16777216),request_sha256:archiveRequestSHA(project,request)};
  const status=readArchiveStatus(value,expected,request);
  const fresh=await journal.read();signal.throwIfAborted();
  if(!fresh||canonicalJSON(fresh)!==expected)throw new Error('INTERCHANGE_ARCHIVE_JOURNAL_CONFLICT');
  if(saved.checkpoint&&status.upload_id!==saved.checkpoint.receipt.upload_id)throw new Error('INTERCHANGE_ARCHIVE_UPLOAD_INVALID');
  return status;
 },options.lock??semanticJournalLock);
}
const executions=new Map<string,Promise<unknown>>();
async function exclusive<T>(key:string,action:()=>Promise<T>,lock:SemanticJournalLock):Promise<T>{
 const previous=executions.get(key)??Promise.resolve();
 const next=previous.catch(()=>{}).then(()=>lock('archive-execution:'+key,action));executions.set(key,next);
 try{return await next;}finally{if(executions.get(key)===next)executions.delete(key);}
}
/** Invoked by explicit user action. Persist intent before Begin and receipt before
 * PUT. Retrying uses the stored key, never silently creates a new operation. */
export async function runArchiveUpload(options:{api:ArchiveUploadControllerAPI;journal:InterchangeArchiveAttemptStore;tenant:string;project:string;request:ArchiveUploadRequest;source:UploadSource;signal:AbortSignal;newKey:()=>string;put?:ArchivePartPut;onProgress?:(done:number,total:number)=>void;lock?:SemanticJournalLock}){
 const {api,journal,tenant,project,signal,source}=options;
 const request=freezeArchiveUpload(options.request,'controller');journal.assertScope(tenant,project,request);
 if(source.name!==request.filename||source.size!==request.bytes||source.sha256!==request.sha256)throw new Error('INTERCHANGE_ARCHIVE_SOURCE_CHANGED');
 return exclusive(journal.key,async()=>{
  signal.throwIfAborted();journal.assertScope(tenant,project,request);
  let record=await journal.read();signal.throwIfAborted();
  const resuming=record!==null&&record.checkpoint!==null;
  if(record===null){record={version:1,key:options.newKey(),body:request,checkpoint:null};await journal.write(record);signal.throwIfAborted();}
  if(record.checkpoint===null){
   const expectedIntent=canonicalJSON(record);
   const beforeBegin=await journal.read();signal.throwIfAborted();
   if(!beforeBegin||canonicalJSON(beforeBegin)!==expectedIntent)throw new Error('INTERCHANGE_ARCHIVE_JOURNAL_CONFLICT');
   const receipt=readArchiveUploadReceipt(await api.beginInterchangeArchive(project,request,record.key,signal),project,request);signal.throwIfAborted();
   const pending=await journal.read();signal.throwIfAborted();
   if(!pending||canonicalJSON(pending)!==expectedIntent)throw new Error('INTERCHANGE_ARCHIVE_JOURNAL_CONFLICT');
   record={...record,checkpoint:{receipt,parts:[],stage:'uploading'}};await journal.write(record);signal.throwIfAborted();
  }
  const active:InterchangeArchiveAttempt=record;
  let expected=canonicalJSON(active);
  const fence=async()=>{signal.throwIfAborted();const fresh=await journal.read();signal.throwIfAborted();if(!fresh||canonicalJSON(fresh)!==expected)throw new Error('INTERCHANGE_ARCHIVE_JOURNAL_CONFLICT');};
  const transport:ArchiveUploadTransport={
   interchangeArchiveParts:async(...args)=>{await fence();const out=await api.interchangeArchiveParts(...args);await fence();return out;},
   completeInterchangeArchive:async(...args)=>{await fence();const out=await api.completeInterchangeArchive(...args);await fence();return out;},
   interchangeArchiveStatus:async(...args)=>{await fence();const out=await api.interchangeArchiveStatus(...args);await fence();return out;},
  };
  const put:ArchivePartPut=async(...args)=>{await fence();const etag=await (options.put??putArchivePart)(...args);await fence();return etag;};
  // A saved uploading receipt may outlive its server state. Do not request
  // more capabilities until fresh authority says this attempt is still open.
  if(resuming&&active.checkpoint!.stage==='uploading'){
   const current=readArchiveStatus(await transport.interchangeArchiveStatus(project,request,active.checkpoint!.receipt,signal),active.checkpoint!.receipt,request);
   if(current.state!=='uploading'||Date.parse(current.expires_at)<=Date.now())return current;
  }
  return executeArchiveUpload({api:transport,project,request,checkpoint:active.checkpoint!,source,signal,put,onProgress:options.onProgress,journal:{save:async(checkpoint,currentSignal)=>{currentSignal.throwIfAborted();await fence();const next={...active,checkpoint};await journal.write(next);expected=canonicalJSON(next);currentSignal.throwIfAborted();await fence();}}});
 },options.lock??semanticJournalLock);
}

export async function abortArchiveUpload(options:{api:{abortInterchangeArchive(project:string,request:ArchiveUploadRequest,receipt:ArchiveUploadReceipt,signal:AbortSignal):Promise<unknown>;interchangeArchiveStatus:ArchiveUploadTransport['interchangeArchiveStatus']};journal:InterchangeArchiveAttemptStore;tenant:string;project:string;request:ArchiveUploadRequest;signal:AbortSignal;lock?:SemanticJournalLock}){
 const {api,journal,tenant,project,signal}=options,request=freezeArchiveUpload(options.request,'abort-controller');journal.assertScope(tenant,project,request);
 return exclusive(journal.key,async()=>{
  signal.throwIfAborted();const record=await journal.read();signal.throwIfAborted();
  if(!record?.checkpoint)throw new Error('INTERCHANGE_ARCHIVE_ABORT_REQUIRES_BEGIN');
  const receipt=readArchiveUploadReceipt(record.checkpoint.receipt,project,request);
  // Persist user cancellation intent BEFORE mutation: a lost response must not
  // allow a remounted uploader to send more parts. Explicit retry sends Abort.
  const aborting:InterchangeArchiveAttempt={...record,checkpoint:{...record.checkpoint,stage:'aborting'}};
  await journal.write(aborting);signal.throwIfAborted();
  const expected=canonicalJSON(aborting);
  const fence=async()=>{signal.throwIfAborted();const fresh=await journal.read();signal.throwIfAborted();if(!fresh||canonicalJSON(fresh)!==expected)throw new Error('INTERCHANGE_ARCHIVE_JOURNAL_CONFLICT');};
  await fence();
  readArchiveAbortAccepted(await api.abortInterchangeArchive(project,request,receipt,signal),receipt);signal.throwIfAborted();
  await fence();
  const status=readArchiveStatus(await api.interchangeArchiveStatus(project,request,receipt,signal),receipt,request);signal.throwIfAborted();
  await fence();return status;
 },options.lock??semanticJournalLock);
}

/** Explicit reset after physical cleanup, never after accepted Abort alone. */
export async function resetAbortedArchiveUpload(options:{api:Pick<ArchiveUploadTransport,'interchangeArchiveStatus'>;journal:InterchangeArchiveAttemptStore;tenant:string;project:string;request:ArchiveUploadRequest;signal:AbortSignal;lock?:SemanticJournalLock}){
 const {api,journal,tenant,project,signal}=options,request=freezeArchiveUpload(options.request,'reset-controller');journal.assertScope(tenant,project,request);
 return exclusive(journal.key,async()=>{
  signal.throwIfAborted();const record=await journal.read();signal.throwIfAborted();
  if(!record?.checkpoint)throw new Error('INTERCHANGE_ARCHIVE_JOURNAL_CONFLICT');
  const receipt=readArchiveUploadReceipt(record.checkpoint.receipt,project,request);
  const status=readArchiveStatus(await api.interchangeArchiveStatus(project,request,receipt,signal),receipt,request);signal.throwIfAborted();
  if(status.state!=='aborted'||status.ready)throw new Error('INTERCHANGE_ARCHIVE_CLEANUP_PENDING');
  const fresh=await journal.read();signal.throwIfAborted();
  if(!fresh?.checkpoint||canonicalJSON(fresh)!==canonicalJSON(record))throw new Error('INTERCHANGE_ARCHIVE_JOURNAL_CONFLICT');
  const aborted:InterchangeArchiveAttempt={...fresh,checkpoint:{...fresh.checkpoint,stage:'aborting'}};
  await journal.write(aborted);signal.throwIfAborted();
  await journal.clearAborted(record.key,receipt.upload_id,aborted);signal.throwIfAborted();
 },options.lock??semanticJournalLock);
}


/** Freshly authorize a ready archive under the same lock as upload/abort/reset.
 * The synchronous handoff happens before releasing this execution scope. */
export async function openReadyArchiveUpload(options:{api:Pick<ArchiveUploadTransport,'interchangeArchiveStatus'>;journal:InterchangeArchiveAttemptStore;tenant:string;project:string;request:ArchiveUploadRequest;signal:AbortSignal;onStatus?:(status:ArchiveStatus)=>void;onReady:(artifact:{artifact_id:string;sha256:string})=>void;lock?:SemanticJournalLock}){
 const {api,journal,tenant,project,signal,onStatus,onReady}=options;
 const request=freezeArchiveUpload(options.request,'handoff');journal.assertScope(tenant,project,request);
 return exclusive(journal.key,async()=>{
  signal.throwIfAborted();journal.assertScope(tenant,project,request);
  const saved=await journal.read();signal.throwIfAborted();
  if(!saved?.checkpoint)throw new Error('INTERCHANGE_ARCHIVE_JOURNAL_CONFLICT');
  assertArchiveHandoffAttempt(saved,saved);
  const status=readArchiveStatus(await api.interchangeArchiveStatus(project,request,saved.checkpoint.receipt,signal),saved.checkpoint.receipt,request);
  signal.throwIfAborted();const current=await journal.read();signal.throwIfAborted();
  journal.assertScope(tenant,project,request);assertArchiveHandoffAttempt(saved,current);
  onStatus?.({...status,...(status.artifact?{artifact:{...status.artifact}}:{})});
  signal.throwIfAborted();journal.assertScope(tenant,project,request);
  if(!status.ready||!status.artifact)throw new Error('INTERCHANGE_ARCHIVE_NOT_READY');
  onReady({...status.artifact});
  return status;
 },options.lock??semanticJournalLock);
}
