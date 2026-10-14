import type {RunOutput,PresentationStatus} from './catalog-types';
import {renderBatchTicket,type RenderBatchTicket} from './render-batch';
import {readRenderRunStatus} from './render-status';
export type RenderFrameResult={run:string;status:string;images:RunOutput[];presentation?:PresentationStatus};
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
/** Only encoded presentation derivatives may be mounted as images. */
export function readRenderImages(value:unknown,now=Date.now()):RunOutput[]{
 if(!value||typeof value!=='object'||Array.isArray(value)||Object.keys(value).join()!=='items'||!Array.isArray((value as {items:unknown}).items))throw new Error('RENDER_OUTPUT_INVALID');
 const items=(value as {items:unknown[]}).items,seen=new Set<string>();
 if(items.length>10000)throw new Error('RENDER_OUTPUT_INVALID');
 const result:RunOutput[]=[];
 for(const item of items){
  if(!item||typeof item!=='object'||Array.isArray(item))throw new Error('RENDER_OUTPUT_INVALID');
  const v=item as Record<string,unknown>,keys=['artifact_id','role','sha256','bytes','media_type','url','expires_at'];
  if(Object.keys(v).length!==keys.length||!keys.every(k=>Object.hasOwn(v,k))||!uuid(v.artifact_id)||seen.has(v.artifact_id.toLowerCase())||typeof v.role!=='string'||typeof v.sha256!=='string'||! /^[a-f0-9]{64}$/.test(v.sha256)||typeof v.bytes!=='number'||!Number.isSafeInteger(v.bytes)||v.bytes<1||typeof v.media_type!=='string'||typeof v.url!=='string'||typeof v.expires_at!=='string')throw new Error('RENDER_OUTPUT_INVALID');
  seen.add(v.artifact_id.toLowerCase());
  let url:URL;try{url=new URL(v.url);}catch{throw new Error('RENDER_OUTPUT_INVALID');}
  const versions=url.searchParams.getAll('versionId');
  const expiry=Date.parse(v.expires_at);
  if(!['https:','http:'].includes(url.protocol)||!url.hostname||url.username||url.password||url.hash||versions.length!==1||!versions[0]||versions[0]==='null'||url.protocol==='http:'&&!['localhost','[::1]'].includes(url.hostname)&&!/^127(?:\.[0-9]{1,3}){3}$/.test(url.hostname)||!Number.isFinite(expiry))throw new Error('RENDER_OUTPUT_INVALID');
  if(v.role==='derivative'&&['image/jpeg','image/webp','image/png'].includes(v.media_type)&&expiry>now)result.push(structuredClone(v) as RunOutput);
 }
 return result;
}
/** Ticket ownership determines which child runs may be read; no parent output substitution. */
export async function loadEditorRenderResults(api:{run:(id:string,signal?:AbortSignal)=>Promise<unknown>;runOutputs:(id:string,signal?:AbortSignal)=>Promise<unknown>},ticket:RenderBatchTicket,signal:AbortSignal):Promise<RenderFrameResult[]>{
 const owned=renderBatchTicket(ticket),result:RenderFrameResult[]=[];
 const active=()=>{if(signal.aborted)throw new Error('RENDER_RESULTS_ABORTED');};
 for(const run of owned.child_run_ids){
  active();const status=readRenderRunStatus(await api.run(run,signal),run,'v2');active();
  const images=['completed','completed_with_errors','waiting_review'].includes(status.status)?readRenderImages(await api.runOutputs(run,signal)):[];
  active();result.push({run,status:status.status,images,...(status.presentation?{presentation:status.presentation}:{})});
 }
 return result;
}

export type PresentationLabelKey='render_presentation_pending'|'render_presentation_ready'|'render_presentation_empty';
const presentationDefaults:Record<PresentationLabelKey,string>={
 render_presentation_pending:'Presentation images are being prepared.',
 render_presentation_ready:'Presentation ready: {ready}/{total}. Failed: {failed}. Cancelled: {cancelled}. Unavailable: {unavailable}.',
 render_presentation_empty:'No presentation image is available. Failed: {failed}. Cancelled: {cancelled}. Unavailable: {unavailable}.',
};
export function renderPresentationLabel(frame:RenderFrameResult,translate:(key:PresentationLabelKey)=>string=key=>presentationDefaults[key]):string{
 const p=frame.presentation;
 if(!p||p.pending>0||p.total===0)return translate('render_presentation_pending');
 let label=translate(p.ready>0?'render_presentation_ready':'render_presentation_empty');
 for(const key of ['ready','total','failed','cancelled','unavailable'] as const)label=label.replaceAll(`{${key}}`,String(p[key]));
 return label;
}
