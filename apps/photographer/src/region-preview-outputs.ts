import type {RunOutput,RunStatus} from './catalog-types';
import {regionJobTicket,type RegionJobTicket} from './region-preview';
export interface RegionRunReader {
 run(id:string,signal?:AbortSignal):Promise<RunStatus>;
 runOutputs(id:string,signal?:AbortSignal):Promise<{items:RunOutput[]}>;
}
function active(signal:AbortSignal){if(signal.aborted)throw new Error('REGION_PREVIEW_ABORTED');}
export function regionPollDelay(ms:number,signal:AbortSignal):Promise<void> {
 active(signal);
 return new Promise((resolve,reject)=>{
  const abort=()=>{clearTimeout(timer);signal.removeEventListener('abort',abort);reject(new Error('REGION_PREVIEW_ABORTED'));};
  const timer=setTimeout(()=>{signal.removeEventListener('abort',abort);resolve();},ms);
  signal.addEventListener('abort',abort,{once:true});
 });
}
/** Fetch only catalog metadata. Image/document bytes require subsequent manifest binding. */
export async function waitRegionOutputs(reader:RegionRunReader,ticket:RegionJobTicket,signal:AbortSignal,delay=regionPollDelay):Promise<{manifest:RunOutput;derivative:RunOutput}> {
 const runId=regionJobTicket(ticket).run_id;
 for(let attempt=0;attempt<90;attempt++){
  active(signal);const run=await reader.run(runId,signal);active(signal);
  if(run.id!==runId)throw new Error('REGION_RUN_BINDING_INVALID');
  if(run.status==='completed'){
   const result=await reader.runOutputs(runId,signal);active(signal);
   if(!Array.isArray(result.items))throw new Error('REGION_OUTPUT_INVALID');
   const documents=result.items.filter(item=>item.role==='document'),images=result.items.filter(item=>item.role==='derivative');
   if(documents.length!==1||images.length!==1)throw new Error('REGION_OUTPUT_INVALID');
   const manifest=documents[0],derivative=images[0];
   for(const [item,maxBytes] of [[manifest,8388608],[derivative,16777216]] as const){
    if(!/^[0-9a-f]{64}$/.test(item.sha256)||!Number.isSafeInteger(item.bytes)||item.bytes<2||item.bytes>maxBytes||!item.artifact_id||!item.url||!Number.isFinite(Date.parse(item.expires_at))||Date.parse(item.expires_at)<=Date.now())throw new Error('REGION_OUTPUT_INVALID');
   }
   if(manifest.artifact_id===derivative.artifact_id||manifest.media_type!=='application/json'||!['image/jpeg','image/webp'].includes(derivative.media_type))throw new Error('REGION_OUTPUT_INVALID');
   return {manifest:{...manifest},derivative:{...derivative}};
  }
  if(!['queued','running'].includes(run.status))throw new Error('REGION_RUN_FAILED');
  if(attempt<89)await delay(1000,signal);
 }
 throw new Error('REGION_PREVIEW_TIMEOUT');
}
