import type {UploadRecord,RemoteUploadState} from '@photoeditor/api-client';
import {archiveUUID} from './interchange-archive-upload';
const object=(v:unknown):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v);
export function readOwnerUploadAccepted(value:unknown,id:string){
 if(!archiveUUID(id)||!object(value)||Object.keys(value).length!==2||value.job_id!==id||value.status!=='queued')throw new Error('UPLOAD_COMPLETE_RESPONSE_INVALID');
 return {job_id:id,status:'queued' as const};
}
export function readOwnerUploadAborted(value:unknown,id:string){
 if(!archiveUUID(id)||!object(value)||value.id!==id||value.state!=='aborted'||typeof value.revision!=='string'||! /^[1-9][0-9]{0,18}$/.test(value.revision)||BigInt(value.revision)>9223372036854775807n)throw new Error('UPLOAD_ABORT_RESPONSE_INVALID');
 return {id,state:'aborted' as const,revision:value.revision};
}
export function readOwnerUploadBegin(value:unknown,record:Pick<UploadRecord,'projectId'|'collectionId'|'name'|'size'>){
 if(!object(value)||!archiveUUID(value.id)||value.project_id!==record.projectId||
 (value.collection_id??undefined)!==record.collectionId||value.filename!==record.name||value.expected_size!==record.size||
 value.part_bytes!==16777216||value.parts_total!==Math.ceil(record.size/16777216)||
 !['initializing','uploading','completing','verifying','verified','rejected','aborted','expired'].includes(value.state as string))throw new Error('UPLOAD_BEGIN_RESPONSE_INVALID');
 return {id:value.id,part_bytes:16777216,parts_total:value.parts_total as number,state:value.state as RemoteUploadState};
}
export function readOwnerUploadParts(value:unknown,id:string,parts:readonly number[],origin:string,now=Date.now()){
 if(!archiveUUID(id)||!Array.isArray(parts)||parts.length<1||parts.length>100||parts.some(n=>!Number.isSafeInteger(n)||n<1||n>32)||new Set(parts).size!==parts.length||!object(value)||value.upload_id!==id||!Array.isArray(value.items)||value.items.length!==parts.length)throw new Error('UPLOAD_PART_RESPONSE_INVALID');
 const expected=new Set(parts);const api=new URL(origin||(typeof location!=='undefined'?location.origin:'https://same-origin.invalid'));
 const loopback=(u:URL)=>['localhost','127.0.0.1','[::1]'].includes(u.hostname);
 return value.items.map(item=>{
  if(!object(item)||typeof item.part_no!=='number'||!expected.delete(item.part_no)||typeof item.url!=='string'||item.url.length>32768||/[\r\n\x00]/.test(item.url)||typeof item.expires_at!=='string')throw new Error('UPLOAD_PART_RESPONSE_INVALID');
  const url=new URL(item.url),expires=Date.parse(item.expires_at);
  if(url.username||url.password||url.hash||!(url.protocol==='https:'||url.protocol==='http:'&&loopback(url)&&api.protocol==='http:'&&loopback(api))||!Number.isFinite(expires)||expires<=now+5000||expires>now+15*60*1000)throw new Error('UPLOAD_PART_RESPONSE_INVALID');
  return {part_no:item.part_no,url:url.href,expires_at:item.expires_at};
 });
}
