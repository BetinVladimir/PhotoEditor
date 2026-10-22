import {ownerUploadFilename} from './upload-filename';
import {putUploadPart} from './upload-part-transport';
import {verifyUploadSource} from "./upload-source-integrity";
import {readOwnerUploadBegin,readOwnerUploadParts,readOwnerUploadAccepted,readOwnerUploadAborted} from './owner-upload-response';
import {ScopedUploadStorage} from './upload-storage';
import AsyncStorage from "@react-native-async-storage/async-storage";
import {ApiClient,APIError,UploadQueue,type MultipartAPI,type UploadRecord,type UploadSource,readUploadAssumeProfile,rawUploadPolicy}from"@photoeditor/api-client";
import {File as NativeFile} from "expo-file-system";
export class OwnerMultipartAPI implements MultipartAPI{constructor(private client:ApiClient,private tenant:string,private onAuthorityLost?:()=>void){}private async request<T>(path:string,options:Parameters<ApiClient["request"]>[1]):Promise<T>{try{return await this.client.request<T>(path,options)}catch(error){if(error instanceof APIError&&[401,403,404].includes(error.status)&&!options?.signal?.aborted)this.onAuthorityLost?.();throw error}}private root(id=""){return`/api/v1/tenants/${encodeURIComponent(this.tenant)}/uploads${id?`/${encodeURIComponent(id)}`:""}`}async begin(r:UploadRecord,signal:AbortSignal){const name=ownerUploadFilename(r.name);const format=(name.split('.').pop()||'').toLowerCase();const scope={projectId:r.projectId,collectionId:r.collectionId,name:ownerUploadFilename(r.name),size:r.size};signal.throwIfAborted();const out=await this.request<unknown>(this.root(),{method:"POST",idempotencyKey:r.localId,signal,body:{project_id:r.projectId,...(r.collectionId!==undefined?{collection_id:r.collectionId}:{}),filename:name,bytes:r.size,format,sha256:r.sha256,...(r.assumeProfile!==undefined?{assume_profile:readUploadAssumeProfile(r.assumeProfile)}:{}),...(r.rawDevelopment!==undefined?{raw_development:rawUploadPolicy(name,r.rawDevelopment)}:{})}});signal.throwIfAborted();return readOwnerUploadBegin(out,scope)}async status(r:UploadRecord,signal:AbortSignal){if(!r.uploadId)throw new Error("UPLOAD_ID_REQUIRED");const scope={projectId:r.projectId,collectionId:r.collectionId,name:ownerUploadFilename(r.name),size:r.size};signal.throwIfAborted();const out=await this.request<unknown>(this.root(r.uploadId),{method:"GET",signal});signal.throwIfAborted();return readOwnerUploadBegin(out,scope)}async sign(id:string,parts:number[],signal:AbortSignal){const requested=[...parts];signal.throwIfAborted();const out=await this.request<unknown>(`${this.root(id)}/parts`,{method:"POST",signal,body:{part_numbers:requested}});signal.throwIfAborted();return readOwnerUploadParts(out,id,requested,this.client.origin)}async put(url:string,bytes:Uint8Array,signal:AbortSignal){return putUploadPart(url,bytes,signal)}async complete(id:string,parts:Array<{part_no:number;etag:string}>,expectedSize:number,signal:AbortSignal){signal.throwIfAborted();const frozen=parts.map(p=>({part_no:p.part_no,etag:p.etag}));const out=await this.request<unknown>(`${this.root(id)}/complete`,{method:"POST",idempotencyKey:`complete:${id}`,signal,body:{parts:frozen,expected_size:expectedSize}});signal.throwIfAborted();readOwnerUploadAccepted(out,id)}async abort(id:string,signal:AbortSignal){signal.throwIfAborted();const out=await this.request<unknown>(`${this.root(id)}/abort`,{method:"POST",signal});signal.throwIfAborted();readOwnerUploadAborted(out,id)}}
function checkReadRange(start:number,end:number,size:number){
 if(!Number.isSafeInteger(start)||!Number.isSafeInteger(end)||start<0||end<=start||end>size||end-start>16777216)throw new Error('UPLOAD_READ_RANGE_INVALID');
}
export async function webSource(file:File,onHash?:(done:number,total:number)=>void,signal=new AbortController().signal):Promise<UploadSource>{
 const source:UploadSource={name:file.name,size:file.size,lastModified:file.lastModified,sha256:'',read:async(start,end,readSignal)=>{
  readSignal.throwIfAborted();checkReadRange(start,end,file.size);const bytes=new Uint8Array(await file.slice(start,end).arrayBuffer());readSignal.throwIfAborted();return bytes;
 }};
 source.verify=current=>verifyUploadSource(source,current);
 source.sha256=await verifyUploadSource({...source,read:async(start,end,current)=>{const bytes=await source.read(start,end,current);onHash?.(end,file.size);return bytes}},signal);
 return source;
}
export async function nativeSource(uri:string,name:string,size:number,lastModified=0,onHash?:(done:number,total:number)=>void,signal=new AbortController().signal):Promise<UploadSource>{
 const source:UploadSource={name,size,lastModified,sha256:'',read:async(start,end,currentSignal)=>{
  currentSignal.throwIfAborted();checkReadRange(start,end,size);const current=new NativeFile(uri);
  if(!current.exists||current.size!==size)throw new Error('UPLOAD_FILE_UNAVAILABLE');
  const handle=current.open();try{handle.offset=start;const bytes=handle.readBytes(end-start);currentSignal.throwIfAborted();if(!(bytes instanceof Uint8Array)||bytes.byteLength!==end-start)throw new Error('UPLOAD_FILE_UNAVAILABLE');return bytes}finally{handle.close()}
 }};
 source.verify=current=>verifyUploadSource(source,current);
 source.sha256=await verifyUploadSource({...source,read:async(start,end,current)=>{
  const bytes=await source.read(start,end,current);onHash?.(end,size);return bytes;
 }},signal);
 return source;
}
export {pickWebFiles} from './web-file-picker';
export const createUploadQueue=(client:ApiClient,origin:string,user:string,tenant:string,project:string,collection?:string,onAuthorityLost?:()=>void)=>new UploadQueue(new OwnerMultipartAPI(client,tenant,onAuthorityLost),new ScopedUploadStorage(AsyncStorage,origin,user,tenant,project,collection),2,2);
