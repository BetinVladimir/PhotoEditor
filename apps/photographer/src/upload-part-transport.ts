const PART_TIMEOUT_MS=120000;
// Signed storage requests carry no application session or redirect authority.
export async function putUploadPart(url:string,bytes:Uint8Array,signal:AbortSignal):Promise<string>{
 signal.throwIfAborted();
 if(!(bytes instanceof Uint8Array)||bytes.byteLength<1||bytes.byteLength>16777216)throw new Error('PART_SIZE_INVALID');
 const controller=new AbortController();
 const abort=()=>controller.abort(signal.reason);
 signal.addEventListener('abort',abort,{once:true});
 const timer=setTimeout(()=>controller.abort(new Error('PART_UPLOAD_TIMEOUT')),PART_TIMEOUT_MS);
 let response:Response|undefined;
 try{
  if(signal.aborted)abort();controller.signal.throwIfAborted();
  response=await fetch(url,{method:'PUT',body:bytes as BodyInit,signal:controller.signal,credentials:'omit',redirect:'error',referrerPolicy:'no-referrer'});
  controller.signal.throwIfAborted();
  if(!response.ok)throw new Error(`PART_UPLOAD_${response.status}`);
  const etag=response.headers.get('ETag')?.replace(/^"|"$/g,'');
  if(!etag||etag.length>256||/[\r\n\x00]/.test(etag))throw new Error('PART_ETAG_INVALID');
  return etag;
 }catch(error){
  if(controller.signal.aborted)throw controller.signal.reason;
  throw error;
 }finally{
  clearTimeout(timer);signal.removeEventListener('abort',abort);
  if(response?.body)void response.body.cancel().catch(()=>{});
 }
}
