import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {sha1} from '@noble/hashes/legacy.js';
import {bytesToHex,hexToBytes} from '@noble/hashes/utils.js';
import {maxInterchangeArchiveBytes} from './interchange-archive-source';
export type ArchiveUploadRequest={filename:string;bytes:number;sha256:string};
export type ArchiveUploadReceipt={upload_id:string;state:'uploading'|'completing'|'verified';part_bytes:number;parts_total:number;request_sha256:string};
export const archiveUUID=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const digest=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
const fail=():never=>{throw new Error('INTERCHANGE_ARCHIVE_UPLOAD_INVALID');};
function fields(v:unknown,keys:string[]):v is Record<string,unknown>{return !!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.prototype.hasOwnProperty.call(v,k));}
export function freezeArchiveUpload(value:unknown,key:string):ArchiveUploadRequest{
 if(typeof key!=='string'||!/^[\x21-\x7e]{1,128}$/.test(key)||!fields(value,['filename','bytes','sha256']))return fail();
 const {filename,bytes,sha256:hash}=value;
 if(typeof filename!=='string'||[...filename].length<5||[...filename].length>240||filename!==filename.normalize('NFC')||/[\\/:\p{Cc}]/u.test(filename)||!filename.toLowerCase().endsWith('.zip')||typeof bytes!=='number'||!Number.isSafeInteger(bytes)||bytes<22||bytes>maxInterchangeArchiveBytes||!digest(hash))return fail();
 return {filename,bytes,sha256:hash};
}
export function archiveRequestSHA(project:string,request:ArchiveUploadRequest):string{
 if(!archiveUUID(project))return fail();const body=freezeArchiveUpload(request,'request-hash');
 return bytesToHex(sha256(new TextEncoder().encode(canonicalJSON({...body,project_id:project}))));
}
export function readArchiveUploadReceipt(value:unknown,project:string,request:ArchiveUploadRequest):ArchiveUploadReceipt{
 const body=freezeArchiveUpload(request,'receipt');
 if(!fields(value,['upload_id','state','part_bytes','parts_total','request_sha256'])||!archiveUUID(value.upload_id)||!['uploading','completing','verified'].includes(value.state as string)||value.part_bytes!==16777216||value.parts_total!==Math.ceil(body.bytes/16777216)||value.request_sha256!==archiveRequestSHA(project,body))return fail();
 return {upload_id:value.upload_id,state:value.state as ArchiveUploadReceipt['state'],part_bytes:16777216,parts_total:value.parts_total as number,request_sha256:value.request_sha256 as string};
}

export type ArchiveUploadedPart={part_no:number;etag:string};
export type ArchivePartURL={part_no:number;url:string;expires_at:string};
export type ArchiveStatus={upload_id:string;state:string;request_sha256:string;expires_at:string;ready:boolean;begin_recovery?:'none'|'reconciliation_required'|'receipt_pending_bind'|'resolved';error_code?:string;artifact?:{artifact_id:string;sha256:string}};
export function archivePartNumbers(receipt:ArchiveUploadReceipt,parts:readonly number[]):number[]{
 if(!Array.isArray(parts)||parts.length<1||parts.length>100)return fail();const seen=new Set<number>();
 return parts.map(n=>{if(!Number.isSafeInteger(n)||n<1||n>receipt.parts_total||seen.has(n))return fail();seen.add(n);return n;});
}
export function archiveCompletionParts(receipt:ArchiveUploadReceipt,parts:readonly ArchiveUploadedPart[]):ArchiveUploadedPart[]{
 if(!Array.isArray(parts)||parts.length!==receipt.parts_total)return fail();
 return parts.map((p,i)=>{if(!fields(p,['part_no','etag'])||p.part_no!==i+1||typeof p.etag!=='string'||p.etag.length<1||p.etag.length>256||/[\x00-\x1f\x7f]/.test(p.etag))return fail();return {part_no:i+1,etag:p.etag};});
}
export function readArchivePartURLs(value:unknown,receipt:ArchiveUploadReceipt,parts:readonly number[],now=Date.now()):ArchivePartURL[]{
 const expected=new Set(archivePartNumbers(receipt,parts));
 if(!Number.isFinite(now)||!fields(value,['parts'])||!Array.isArray(value.parts)||value.parts.length!==expected.size)return fail();
 return value.parts.map((p:unknown)=>{
  if(!fields(p,['part_no','url','expires_at'])||typeof p.part_no!=='number'||!expected.delete(p.part_no)||typeof p.url!=='string'||new TextEncoder().encode(p.url).byteLength>32768||/[\r\n\x00]/.test(p.url)||!/^https?:\/\//.test(p.url)||p.url.split('/')[2].includes('@')||typeof p.expires_at!=='string'||!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.test(p.expires_at)||!Number.isFinite(Date.parse(p.expires_at))||Date.parse(p.expires_at)<=now+5000||Date.parse(p.expires_at)>now+15*60*1000)return fail();
  let url:URL;try{url=new URL(p.url);}catch{return fail();}
  if(!['http:','https:'].includes(url.protocol)||url.username||url.password||url.hash||
     (url.protocol==='http:'&&!['localhost','[::1]'].includes(url.hostname)&&!/^127(?:\.[0-9]{1,3}){3}$/.test(url.hostname)))return fail();
  return {part_no:p.part_no,url:url.href,expires_at:p.expires_at};
 });
}
export function readArchiveAccepted(value:unknown,receipt:ArchiveUploadReceipt):{upload_id:string;accepted:true}{
 if(!fields(value,['upload_id','accepted'])||value.upload_id!==receipt.upload_id||value.accepted!==true)return fail();
 return {upload_id:receipt.upload_id,accepted:true};
}
/** Matches uuid.NewSHA1(upload, []byte("archive:artifact:1")) on the server. */
export function archiveArtifactID(upload:string):string{
 if(!archiveUUID(upload))return fail();
 const namespace=hexToBytes(upload.replaceAll('-','')),name=new TextEncoder().encode('archive:artifact:1');
 const bytes=new Uint8Array(16+name.length);bytes.set(namespace);bytes.set(name,16);
 const hash=sha1(bytes).slice(0,16);hash[6]=(hash[6]&15)|80;hash[8]=(hash[8]&63)|128;
 const hex=bytesToHex(hash);return `${hex.slice(0,8)}-${hex.slice(8,12)}-${hex.slice(12,16)}-${hex.slice(16,20)}-${hex.slice(20)}`;
}
/** Matches uuid.NewSHA1(tenant, []byte("interchange:archive-upload:"+user+":"+key)). */
export function archiveUploadID(tenant:string,user:string,key:string):string{
 if(!archiveUUID(tenant)||!archiveUUID(user)||!/^[\x21-\x7e]{1,128}$/.test(key))return fail();
 const namespace=hexToBytes(tenant.replaceAll('-','')),name=new TextEncoder().encode(`interchange:archive-upload:${user}:${key}`);
 const bytes=new Uint8Array(16+name.length);bytes.set(namespace);bytes.set(name,16);
 const hash=sha1(bytes).slice(0,16);hash[6]=(hash[6]&15)|80;hash[8]=(hash[8]&63)|128;
 const hex=bytesToHex(hash);return `${hex.slice(0,8)}-${hex.slice(8,12)}-${hex.slice(12,16)}-${hex.slice(16,20)}`;
}
export function readArchiveStatus(value:unknown,receipt:ArchiveUploadReceipt,request:ArchiveUploadRequest):ArchiveStatus{
 if(!value||typeof value!=='object'||Array.isArray(value))return fail();const v=value as Record<string,unknown>;
 if(Object.keys(v).some(k=>!['upload_id','state','request_sha256','expires_at','ready','begin_recovery','error_code','artifact'].includes(k))||v.upload_id!==receipt.upload_id||v.request_sha256!==receipt.request_sha256||typeof v.state!=='string'||!['reserved','uploading','completing','verified','aborting','aborted','failed'].includes(v.state)||typeof v.expires_at!=='string'||!Number.isFinite(Date.parse(v.expires_at))||typeof v.ready!=='boolean')return fail();
 const out:ArchiveStatus={upload_id:receipt.upload_id,state:v.state,request_sha256:receipt.request_sha256,expires_at:v.expires_at,ready:v.ready};
 if(v.begin_recovery!==undefined){if(typeof v.begin_recovery!=='string'||!['none','reconciliation_required','receipt_pending_bind','resolved'].includes(v.begin_recovery)||['reconciliation_required','receipt_pending_bind'].includes(v.begin_recovery)&&v.state!=='reserved'||v.begin_recovery==='resolved'&&v.state==='reserved')return fail();out.begin_recovery=v.begin_recovery as ArchiveStatus['begin_recovery'];}
 if(v.error_code!==undefined){if(typeof v.error_code!=='string'||!['ARCHIVE_INTEGRITY','UPLOAD_ABORTED','UPLOAD_EXPIRED'].includes(v.error_code))return fail();out.error_code=v.error_code;}
 if((v.state==='aborting'||v.state==='aborted')!==(out.error_code!==undefined))return fail();
 if(v.ready){if(v.state!=='verified'||!fields(v.artifact,['artifact_id','sha256'])||v.artifact.artifact_id!==archiveArtifactID(receipt.upload_id)||v.artifact.sha256!==request.sha256)return fail();out.artifact={artifact_id:v.artifact.artifact_id,sha256:request.sha256};}
 else if(v.artifact!==undefined)return fail();
 return out;
}

export function readArchiveAbortAccepted(value:unknown,receipt:ArchiveUploadReceipt):{upload_id:string;accepted:true;cleanup_pending:boolean}{
 if(!fields(value,['upload_id','accepted','cleanup_pending'])||value.upload_id!==receipt.upload_id||value.accepted!==true||typeof value.cleanup_pending!=='boolean')return fail();
 return {upload_id:receipt.upload_id,accepted:true,cleanup_pending:value.cleanup_pending};
}
