import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import type {UploadSource} from '@photoeditor/api-client';
export const maxInterchangeArchiveBytes=64*1024*1024*1024;
const chunkBytes=1024*1024;
export type ArchiveFile={name:string;size:number;lastModified:number;slice(start:number,end:number):{arrayBuffer():Promise<ArrayBuffer>}};
/** Hashes without retaining the whole archive. This is source preparation only;
 * server multipart completion and typed archive admission remain authoritative. */
export async function interchangeArchiveSource(file:ArchiveFile,signal:AbortSignal,onHash?:(done:number,total:number)=>void):Promise<UploadSource>{
 signal.throwIfAborted();
 const {name,size,lastModified}=file;
 if(typeof name!=='string'||[...name].length<5||[...name].length>240||name!==name.normalize('NFC')||/[\\/:\p{Cc}]/u.test(name)||!name.toLowerCase().endsWith('.zip')||!Number.isSafeInteger(size)||size<22||size>maxInterchangeArchiveBytes||!Number.isSafeInteger(lastModified)||lastModified<0)throw new Error('INTERCHANGE_ARCHIVE_SOURCE_INVALID');
 const unchanged=()=>{if(file.name!==name||file.size!==size||file.lastModified!==lastModified)throw new Error('INTERCHANGE_ARCHIVE_SOURCE_CHANGED');};
 const read=async(start:number,end:number,active:AbortSignal)=>{
  active.throwIfAborted();unchanged();
  if(!Number.isSafeInteger(start)||!Number.isSafeInteger(end)||start<0||end<=start||end>size||end-start>64*1024*1024)throw new Error('INTERCHANGE_ARCHIVE_RANGE_INVALID');
  const result=new Uint8Array(await file.slice(start,end).arrayBuffer());active.throwIfAborted();unchanged();
  if(result.byteLength!==end-start)throw new Error('INTERCHANGE_ARCHIVE_SOURCE_CHANGED');return result;
 };
 const hash=sha256.create();
 for(let start=0;start<size;start+=chunkBytes){const end=Math.min(size,start+chunkBytes);hash.update(await read(start,end,signal));onHash?.(end,size);signal.throwIfAborted();}
 unchanged();signal.throwIfAborted();
 return {name,size,lastModified,sha256:bytesToHex(hash.digest()),read};
}
