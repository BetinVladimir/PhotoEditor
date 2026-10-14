import {sha256} from "@noble/hashes/sha2.js";
import {bytesToHex} from "@noble/hashes/utils.js";
import type {UploadSource} from "@photoeditor/api-client";

// A macrotask boundary lets native navigation and pause/abort handlers run.
async function yieldForCancellation(signal:AbortSignal):Promise<void>{
  signal.throwIfAborted();
  await new Promise<void>((resolve,reject)=>{
    const cancel=()=>{clearTimeout(timer);signal.removeEventListener('abort',cancel);reject(signal.reason)};
    const timer=setTimeout(()=>{signal.removeEventListener('abort',cancel);resolve()},0);
    signal.addEventListener('abort',cancel,{once:true});
    if(signal.aborted)cancel();
  });
  signal.throwIfAborted();
}
// Re-read through the same bounded source adapter used for multipart reads.
export async function verifyUploadSource(source:UploadSource,signal:AbortSignal):Promise<string>{
  const size=source.size,read=source.read.bind(source),hash=sha256.create();
  if(!Number.isSafeInteger(size)||size<1||size>536870912)throw new Error("UPLOAD_FILE_UNAVAILABLE");
  for(let start=0;start<size;start+=1024*1024){
    signal.throwIfAborted();
    const end=Math.min(size,start+1024*1024),bytes=await read(start,end,signal);
    signal.throwIfAborted();
    if(!(bytes instanceof Uint8Array)||bytes.byteLength!==end-start)throw new Error("UPLOAD_FILE_UNAVAILABLE");
    hash.update(bytes);
    if(end<size)await yieldForCancellation(signal);
  }
  signal.throwIfAborted();
  return bytesToHex(hash.digest());
}
