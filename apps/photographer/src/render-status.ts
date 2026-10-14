import type {RunStatus} from './catalog-types';
const states=['draft','queued','running','waiting_review','cancelling','cancelled','completed','completed_with_errors','failed'];
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
function fields(v:unknown,keys:string[],optional:string[]=[]):v is Record<string,unknown>{return !!v&&typeof v==='object'&&!Array.isArray(v)&&keys.every(k=>Object.prototype.hasOwnProperty.call(v,k))&&Object.keys(v).every(k=>keys.includes(k)||optional.includes(k));}
/** Parent status is distinct from the saved admission ticket. */
export function readRenderRunStatus(v:unknown,run:string,kind:'bulk'|'v2'='bulk'):RunStatus{
 if(!uuid(run)||!fields(v,['id','kind','status','revision','counts','stage','progress','errors'],['output_collection_id','presentation'])||!uuid(v.id)||v.id.toLowerCase()!==run.toLowerCase()||v.kind!==kind||typeof v.status!=='string'||!states.includes(v.status)||typeof v.revision!=='string'||!/^[1-9][0-9]{0,18}$/.test(v.revision)||BigInt(v.revision)>9223372036854775807n||typeof v.stage!=='string'||typeof v.progress!=='number'||!Number.isFinite(v.progress)||v.progress<0||v.progress>1||!Array.isArray(v.errors)||v.errors.some(e=>typeof e!=='string')||!fields(v.counts,['total','succeeded','failed','skipped','review']))throw new Error('RENDER_STATUS_INVALID');
 const counts=v.counts;
 if(Object.values(counts).some(n=>typeof n!=='number'||!Number.isSafeInteger(n)||n<0)||['succeeded','failed','skipped','review'].some(k=>(counts[k] as number)>(counts.total as number))||('output_collection_id' in v&&!uuid(v.output_collection_id)))throw new Error('RENDER_STATUS_INVALID');
 if('presentation' in v){
  const p=v.presentation;
  if(kind!=='v2'||!fields(p,['total','pending','ready','failed','cancelled','unavailable'])||Object.values(p).some(n=>typeof n!=='number'||!Number.isSafeInteger(n)||n<0)||['pending','ready','failed','cancelled','unavailable'].reduce((sum,k)=>sum+BigInt(p[k] as number),0n)!==BigInt(p.total as number))throw new Error('RENDER_STATUS_INVALID');
 }
 return structuredClone(v) as RunStatus;
}
const stateKeys={draft:'render_state_draft',queued:'render_state_queued',running:'render_state_running',waiting_review:'render_state_waiting_review',cancelling:'render_state_cancelling',cancelled:'render_state_cancelled',completed:'render_state_completed',completed_with_errors:'render_state_completed_with_errors',failed:'render_state_failed'} as const;
export type RenderStateKey=typeof stateKeys[keyof typeof stateKeys];
export function renderStateLabel(state:string,translate?:(key:RenderStateKey)=>string):string{
 const labels:Record<string,string>={draft:'Preparing',queued:'Queued',running:'Processing',waiting_review:'Ready for review',cancelling:'Cancelling',cancelled:'Cancelled',completed:'Completed',completed_with_errors:'Completed with errors',failed:'Failed'};
 const key=stateKeys[state as keyof typeof stateKeys];
 return key&&translate?translate(key):labels[state]??state;
}
export function renderStatusLabel(status:RunStatus,translate?:(key:RenderStateKey)=>string):string{
 return `${renderStateLabel(status.status,translate)} (${Math.round(status.progress*100)}%)`;
}

/** Ignore duplicate or older observations; revisions are int64 strings, not JS numbers. */
export function advanceRenderRunStatus(previous:RunStatus|null,candidate:unknown,run:string):RunStatus{
 const next=readRenderRunStatus(candidate,run);
 if(!previous)return next;
 const current=readRenderRunStatus(previous,run);
 return BigInt(next.revision)>BigInt(current.revision)?next:current;
}
