import {APIError} from '@photoeditor/api-client';
import type {RenderFrameResult} from './editor-render-results';

/** Serialized foreground observation also refreshes expiring signed URLs.
 * A completed Render run may still be publishing its presentation derivatives.
 */
export function watchRenderResults(options:{read:(signal:AbortSignal)=>Promise<RenderFrameResult[]>;observe:(frames:RenderFrameResult[])=>void;error?:(error:unknown)=>void}):()=>void{
 let stopped=false,timer:ReturnType<typeof setTimeout>|undefined,request:AbortController|undefined,failures=0;
 const stop=()=>{stopped=true;if(timer!==undefined)clearTimeout(timer);request?.abort();};
 async function poll(){
  if(stopped)return;
  const controller=new AbortController();request=controller;let delay=10000;
  try{
   const frames=await options.read(controller.signal);
   if(stopped||controller.signal.aborted)return;
   options.observe(frames);failures=0;
   if(frames.length>0&&frames.every(frame=>['failed','cancelled'].includes(frame.status)||(frame.images.length===0&&!!frame.presentation&&frame.presentation.total>0&&frame.presentation.pending===0&&frame.presentation.ready===0))){stop();return;}
  }catch(error){
   if(stopped||controller.signal.aborted)return;
   // A failed refresh cannot keep an older signed candidate visible after a
   // review decision or authority change that may have happened meanwhile.
   options.observe([]);
   if(error instanceof APIError&&[401,403,404,410].includes(error.status)){options.error?.(error);stop();return;}
   options.error?.(error);
   failures++;delay=Math.min(30000,10000*2**Math.min(failures,2));
  }finally{if(request===controller)request=undefined;}
  if(!stopped)timer=setTimeout(()=>{timer=undefined;void poll();},delay);
 }
 void poll();return stop;
}
