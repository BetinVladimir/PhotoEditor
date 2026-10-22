import {APIError} from '@photoeditor/api-client';
export type EditorSourcePreview={url:string;expires_at:string};
/** Own both transport and signed-link lifetimes within one foreground scope. */
export function watchEditorSourcePreview(options:{read:(signal:AbortSignal)=>Promise<EditorSourcePreview>;ready:(url:string)=>void;failed:()=>void}):()=>void{
 let stopped=false,attempts=0,transientFailures=0,timer:ReturnType<typeof setTimeout>|undefined,request:AbortController|undefined,deadline:ReturnType<typeof setTimeout>|undefined;
 function stop(){stopped=true;if(timer!==undefined)clearTimeout(timer);if(deadline!==undefined)clearTimeout(deadline);request?.abort();}
 function fail(){if(stopped)return;stop();options.ready('');options.failed();}
 async function poll(){
  if(stopped)return;const controller=new AbortController();request=controller;attempts++;
  deadline=setTimeout(()=>{if(request===controller)fail();},8000);
  try{
   const link=await options.read(controller.signal);if(stopped||controller.signal.aborted)return;
   const expires=Date.parse(link.expires_at),remaining=expires-Date.now();
   if(!link.url||!Number.isFinite(expires)||remaining<=0)throw new Error('EDITOR_SOURCE_PREVIEW_EXPIRED');
   options.ready(link.url);attempts=0;transientFailures=0;
   // Clear the old bearer at expiry before requesting its replacement.
   timer=setTimeout(()=>{timer=undefined;if(stopped)return;options.ready('');void poll();},Math.min(2147483647,remaining));
  }catch(error){
   if(stopped||controller.signal.aborted)return;
   if(error instanceof APIError&&error.status===409&&error.code==='MEDIA_NOT_READY'&&attempts<90){timer=setTimeout(()=>{timer=undefined;void poll();},2000);}
   else if(error instanceof APIError&&[502,503,504].includes(error.status)&&++transientFailures<5){
    options.ready('');timer=setTimeout(()=>{timer=undefined;void poll();},Math.min(16000,1000*2**transientFailures));
   }else fail();
  }finally{if(request===controller){if(deadline!==undefined)clearTimeout(deadline);deadline=undefined;request=undefined;}}
 }
 void poll();return stop;
}
