import {canonicalJSON,type UploadSource} from '@photoeditor/api-client';
import {archiveCompletionParts,freezeArchiveUpload,readArchiveAccepted,readArchivePartURLs,readArchiveStatus,readArchiveUploadReceipt,type ArchiveUploadRequest,type ArchiveUploadReceipt,type ArchiveUploadedPart,type ArchiveStatus} from './interchange-archive-upload';
export type ArchiveCheckpoint={receipt:ArchiveUploadReceipt;parts:ArchiveUploadedPart[];stage:'uploading'|'queued'|'aborting'};
export interface ArchiveUploadTransport{
 interchangeArchiveParts(project:string,request:ArchiveUploadRequest,receipt:ArchiveUploadReceipt,parts:number[],signal:AbortSignal):Promise<unknown>;
 completeInterchangeArchive(project:string,request:ArchiveUploadRequest,receipt:ArchiveUploadReceipt,parts:ArchiveUploadedPart[],signal:AbortSignal):Promise<unknown>;
 interchangeArchiveStatus(project:string,request:ArchiveUploadRequest,receipt:ArchiveUploadReceipt,signal:AbortSignal):Promise<unknown>;
}
export interface ArchiveCheckpointWriter{save(checkpoint:ArchiveCheckpoint,signal:AbortSignal):Promise<void>}
export type ArchivePartPut=(url:string,bytes:Uint8Array,signal:AbortSignal)=>Promise<string>;
export const putArchivePart:ArchivePartPut=async(url,bytes,signal)=>{
 signal.throwIfAborted();const response=await fetch(url,{method:'PUT',body:bytes as BodyInit,credentials:'omit',redirect:'error',signal});signal.throwIfAborted();
 if(!response.ok)throw new Error(`INTERCHANGE_ARCHIVE_PART_${response.status}`);
 const etag=response.headers.get('ETag')?.replace(/^"|"$/g,'');
 if(!etag||etag.length>256||/[\r\n\x00]/.test(etag))throw new Error('INTERCHANGE_ARCHIVE_ETAG_INVALID');return etag;
};
/** One sequential16MiB part at a time; durable receipt precedes the next part.
 * Caller owns exclusive journal scope and persists Begin receipt before entry.
 * No retries or polling run invisibly; repeat invocation resumes saved parts. */
export async function executeArchiveUpload(options:{api:ArchiveUploadTransport;journal:ArchiveCheckpointWriter;project:string;request:ArchiveUploadRequest;checkpoint:ArchiveCheckpoint;source:UploadSource;signal:AbortSignal;put?:ArchivePartPut;onProgress?:(done:number,total:number)=>void}):Promise<ArchiveStatus>{
 const {api,journal,project,source,signal}=options;signal.throwIfAborted();
 const request=freezeArchiveUpload(options.request,'executor'),receipt=readArchiveUploadReceipt(options.checkpoint.receipt,project,request);
 const stage=options.checkpoint.stage;
 const requestPort=async<T>(call:(candidate:ArchiveUploadRequest)=>Promise<T>):Promise<T>=>{
  const candidate={...request},expected=canonicalJSON(request);
  const result=await call(candidate);signal.throwIfAborted();
  if(canonicalJSON(candidate)!==expected)throw new Error('INTERCHANGE_ARCHIVE_REQUEST_CHANGED');
  return result;
 };
 const {name,size,sha256,lastModified}=source;
 const unchanged=()=>{signal.throwIfAborted();if(source.name!==name||source.size!==size||source.sha256!==sha256||source.lastModified!==lastModified||name!==request.filename||size!==request.bytes||sha256!==request.sha256)throw new Error('INTERCHANGE_ARCHIVE_SOURCE_CHANGED');};unchanged();
 const parts=options.checkpoint.parts.map(p=>({...p})).sort((a,b)=>a.part_no-b.part_no),seen=new Set<number>();
 for(const p of parts){if(!Number.isSafeInteger(p.part_no)||p.part_no<1||p.part_no>receipt.parts_total||seen.has(p.part_no)||typeof p.etag!=='string'||!p.etag||p.etag.length>256||/[\r\n\x00]/.test(p.etag))throw new Error('INTERCHANGE_ARCHIVE_CHECKPOINT_INVALID');seen.add(p.part_no);}
 const save=async(stage:ArchiveCheckpoint['stage'])=>{unchanged();await journal.save({receipt:{...receipt},parts:parts.map(p=>({...p})),stage},signal);unchanged();};
 if(!['uploading','queued','aborting'].includes(stage))throw new Error('INTERCHANGE_ARCHIVE_CHECKPOINT_INVALID');
 if(stage!=='uploading'||receipt.state!=='uploading'){
  if(stage==='queued')archiveCompletionParts(receipt,parts);
  const value=await requestPort(candidate=>api.interchangeArchiveStatus(project,candidate,{...receipt},signal));unchanged();return readArchiveStatus(value,receipt,request);
 }
 const put=options.put??putArchivePart;
 await save('uploading');
 for(let n=1;n<=receipt.parts_total;n++){
  if(seen.has(n))continue;unchanged();
  const links=await requestPort(candidate=>api.interchangeArchiveParts(project,candidate,{...receipt},[n],signal));unchanged();
  // OwnerAPI returns the validated array; validate again at this port boundary.
  const link=readArchivePartURLs({parts:links},receipt,[n])[0];
  const start=(n-1)*receipt.part_bytes,end=Math.min(size,start+receipt.part_bytes);
  const bytes=await source.read(start,end,signal);unchanged();if(bytes.byteLength!==end-start)throw new Error('INTERCHANGE_ARCHIVE_SOURCE_CHANGED');
  // File reads may yield long enough for a signed URL to expire.
  readArchivePartURLs({parts:[link]},receipt,[n]);
  const etag=await put(link.url,bytes,signal);unchanged();if(!etag||etag.length>256||/[\r\n\x00]/.test(etag))throw new Error('INTERCHANGE_ARCHIVE_ETAG_INVALID');
  parts.push({part_no:n,etag});parts.sort((a,b)=>a.part_no-b.part_no);seen.add(n);await save('uploading');
  options.onProgress?.(parts.reduce((sum,p)=>sum+Math.min(receipt.part_bytes,size-(p.part_no-1)*receipt.part_bytes),0),size);unchanged();
 }
 const complete=archiveCompletionParts(receipt,parts);
 const expectedCompletion=canonicalJSON(complete);
 const accepted=await requestPort(candidate=>api.completeInterchangeArchive(project,candidate,{...receipt},complete,signal));unchanged();
 if(canonicalJSON(complete)!==expectedCompletion)throw new Error('INTERCHANGE_ARCHIVE_COMPLETION_CHANGED');
 readArchiveAccepted(accepted,receipt);await save('queued');
 const value=await requestPort(candidate=>api.interchangeArchiveStatus(project,candidate,{...receipt},signal));unchanged();return readArchiveStatus(value,receipt,request);
}
