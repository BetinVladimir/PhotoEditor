import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import type {MultiPreview} from './multi-preview';
export async function downloadMultiPreview(value:MultiPreview,signal:AbortSignal,transport:typeof fetch=fetch):Promise<Uint8Array>{
 const ref=structuredClone(value),active=()=>{if(signal.aborted)throw new Error('MULTI_PREVIEW_ABORTED');};active();
 if(!Number.isSafeInteger(ref.bytes)||ref.bytes<2||ref.bytes>54525952||!Number.isFinite(Date.parse(ref.expires_at))||Date.parse(ref.expires_at)<=Date.now()||!/^[0-9a-f]{64}$/.test(ref.artifact.sha256)||!['image/jpeg','image/webp'].includes(ref.media_type))throw new Error('MULTI_PREVIEW_INVALID');
 const url=new URL(ref.url),versions=url.searchParams.getAll('versionId');if(!['https:','http:'].includes(url.protocol)||!url.hostname||url.username||url.password||url.hash||versions.length!==1||!versions[0]||versions[0]==='null'||url.protocol==='http:'&&!['localhost','[::1]'].includes(url.hostname)&&!/^127(?:\.[0-9]{1,3}){3}$/.test(url.hostname))throw new Error('MULTI_PREVIEW_INVALID');
 const response=await transport(url.href,{signal,credentials:'omit',redirect:'error',referrerPolicy:'no-referrer',cache:'no-store'});if(signal.aborted){await response.body?.cancel();active();}
 const length=response.headers.get('content-length'),mime=response.headers.get('content-type')?.split(';')[0].trim();
 if(response.status!==200||length!==null&&(!/^\d+$/.test(length)||Number(length)!==ref.bytes)||mime!==ref.media_type){await response.body?.cancel();throw new Error('MULTI_PREVIEW_HTTP');}
 if(!response.body)throw new Error('MULTI_PREVIEW_STREAM_UNAVAILABLE');
 const reader=response.body.getReader(),bytes=new Uint8Array(ref.bytes),hash=sha256.create();let offset=0,complete=false;
 const abort=()=>{void reader.cancel().catch(()=>undefined);};signal.addEventListener('abort',abort,{once:true});
 try{
  while(true){active();const chunk=await reader.read();active();if(chunk.done)break;
   if(!(chunk.value instanceof Uint8Array)||chunk.value.length===0||chunk.value.length>ref.bytes-offset)throw new Error('MULTI_PREVIEW_SIZE');
   bytes.set(chunk.value,offset);hash.update(chunk.value);offset+=chunk.value.length;
  }
  if(offset!==ref.bytes||bytesToHex(hash.digest())!==ref.artifact.sha256)throw new Error('MULTI_PREVIEW_DIGEST');
  complete=true;return bytes;
 }finally{signal.removeEventListener('abort',abort);if(!complete)await reader.cancel().catch(()=>undefined);reader.releaseLock();}
}
