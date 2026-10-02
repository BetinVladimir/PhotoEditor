import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import type {RunOutput} from './catalog-types';
function active(signal:AbortSignal){if(signal.aborted)throw new Error('REGION_PREVIEW_ABORTED');}
/** Signed catalog links only; stream into one declared-size allocation. */
export async function readRegionArtifact(reference:RunOutput,kind:'manifest'|'derivative',signal:AbortSignal,transport:typeof fetch=fetch):Promise<Uint8Array> {
 const ref={...reference},limit=kind==='manifest'?8388608:16777216;
 active(signal);
 if(!Number.isSafeInteger(ref.bytes)||ref.bytes<2||ref.bytes>limit||!/^[0-9a-f]{64}$/.test(ref.sha256)||!Number.isFinite(Date.parse(ref.expires_at))||Date.parse(ref.expires_at)<=Date.now()||ref.role!==(kind==='manifest'?'document':'derivative')||(kind==='manifest'?ref.media_type!=='application/json':!['image/jpeg','image/webp'].includes(ref.media_type)))throw new Error('REGION_ARTIFACT_INVALID');
 const url=new URL(ref.url);
 if(!['https:','http:'].includes(url.protocol)||url.username||url.password||url.hash)throw new Error('REGION_ARTIFACT_INVALID');
 const response=await transport(url.href,{signal,credentials:'omit',redirect:'error',referrerPolicy:'no-referrer',cache:'no-store'});
 active(signal);
 if(response.status!==200){await response.body?.cancel();throw new Error('REGION_ARTIFACT_HTTP');}
 const length=response.headers.get('content-length');
 if(length!==null&&(!/^\d+$/.test(length)||Number(length)!==ref.bytes)){await response.body?.cancel();throw new Error('REGION_ARTIFACT_SIZE');}
 if(!response.body)throw new Error('REGION_STREAM_UNAVAILABLE');
 const reader=response.body.getReader(),output=new Uint8Array(ref.bytes),hash=sha256.create();let offset=0,finished=false;
 const abort=()=>{void reader.cancel().catch(()=>undefined);};signal.addEventListener('abort',abort,{once:true});
 try{
  while(true){active(signal);const chunk=await reader.read();active(signal);if(chunk.done)break;
   if(chunk.value.byteLength>ref.bytes-offset)throw new Error('REGION_ARTIFACT_SIZE');
   output.set(chunk.value,offset);offset+=chunk.value.byteLength;hash.update(chunk.value);
  }
  if(offset!==ref.bytes)throw new Error('REGION_ARTIFACT_SIZE');
  if(bytesToHex(hash.digest())!==ref.sha256)throw new Error('REGION_ARTIFACT_SHA');
  finished=true;return output;
 }finally{signal.removeEventListener('abort',abort);if(!finished)await reader.cancel().catch(()=>undefined);reader.releaseLock();}
}
