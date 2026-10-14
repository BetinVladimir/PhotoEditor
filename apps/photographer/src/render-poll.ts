import {APIError} from '@photoeditor/api-client';
import type {RunStatus} from './catalog-types';
import {advanceRenderRunStatus} from './render-status';
const terminal=new Set(['completed','completed_with_errors','cancelled','failed']);
/** One request at a time. Stopping cancels both transport and scheduled retries. */
export function watchRenderRun(options:{run:string;read:(run:string,signal:AbortSignal)=>Promise<unknown>;current:()=>RunStatus|null;observe:(status:RunStatus)=>void;error:(error:unknown)=>void}):()=>void{
 let stopped=false,timer:ReturnType<typeof setTimeout>|undefined,request:AbortController|undefined,failures=0;
 const stop=()=>{stopped=true;if(timer!==undefined)clearTimeout(timer);request?.abort();};
 const schedule=(delay:number)=>{if(!stopped)timer=setTimeout(()=>{timer=undefined;void poll();},delay);};
 async function poll(){
  if(stopped)return;
  if(terminal.has(options.current()?.status??'')){stop();return;}
  const controller=new AbortController();request=controller;
  let delay=2000;
  try{
   const wire=await options.read(options.run,controller.signal);
   if(stopped||controller.signal.aborted)return;
   const status=advanceRenderRunStatus(options.current(),wire,options.run);
   options.observe(status);failures=0;
   if(terminal.has(status.status)){stop();return;}
   if(status.status==='waiting_review')delay=10000;
  }catch(error){
   if(stopped||controller.signal.aborted)return;
   options.error(error);
   if(error instanceof APIError&&[401,403,404,410].includes(error.status)){stop();return;}
   failures++;delay=Math.min(30000,2000*2**Math.min(failures,4));
  }finally{if(request===controller)request=undefined;}
  schedule(delay);
 }
 void poll();return stop;
}
