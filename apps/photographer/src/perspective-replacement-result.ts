import {sha1} from '@noble/hashes/legacy.js';
import type {OwnerAPI} from './owner-api';
import type {V2VersionBinding} from './catalog-types';
import {perspectiveProposalScope,readPerspectiveProposal} from './perspective-proposal';
import type {PerspectiveResultObservation} from './perspective-result';

export type PerspectiveReplacementObservation=PerspectiveResultObservation|{state:'terminal';workflow_state:'failed'|'cancelled'|'completed_with_errors'};
export type PerspectiveReplacementResultScope={tenant:string;project:string;review:string;decisionHash:string;originalRun:string;originalTask:string;run:string;workflow:string;task:string;base:V2VersionBinding};
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
function uuid5(namespace:string,name:string):string {
 const hex=namespace.replaceAll('-',''),text=new TextEncoder().encode(name),bytes=new Uint8Array(16+text.length);
 for(let i=0;i<16;i++)bytes[i]=parseInt(hex.slice(i*2,i*2+2),16);
 bytes.set(text,16);const hash=sha1(bytes).slice(0,16);hash[6]=(hash[6]&15)|80;hash[8]=(hash[8]&63)|128;
 const out=Array.from(hash,v=>v.toString(16).padStart(2,'0')).join('');
 return `${out.slice(0,8)}-${out.slice(8,12)}-${out.slice(12,16)}-${out.slice(16,20)}-${out.slice(20)}`;
}
export function validatePerspectiveReplacementScope(owned:PerspectiveReplacementResultScope):void {
 if(![owned.tenant,owned.project,owned.review,owned.originalRun,owned.originalTask,owned.run,owned.workflow,owned.task].every(uuid)||!/^[0-9a-f]{64}$/.test(owned.decisionHash))throw new Error('PERSPECTIVE_REPLACEMENT_SCOPE_INVALID');
 perspectiveProposalScope(owned.tenant,owned.project,owned.task,owned.base);
 const namespace=uuid5(owned.tenant,`V29.NegativeReplacement/v2:${owned.project}:${owned.review}:${owned.decisionHash}`);
 if(owned.workflow!==uuid5(owned.run,'workflow')||owned.run!==uuid5(namespace,`run:${owned.originalRun}:${owned.originalTask}`)||owned.task!==uuid5(namespace,`task:${owned.originalTask}`))throw new Error('PERSPECTIVE_REPLACEMENT_SCOPE_INVALID');
}
export function readPerspectiveReplacementScope(value:unknown,tenant:string,project:string,review:string,originalTask:string,decisionHash:string):PerspectiveReplacementResultScope {
 const keys=['tenant_id','project_id','review_id','decision_hash','original_run_id','original_task_id','run_id','workflow_id','task_id','base'];
 if(!value||typeof value!=='object'||Array.isArray(value)||Object.keys(value).length!==keys.length||!keys.every(k=>Object.prototype.hasOwnProperty.call(value,k)))throw new Error('PERSPECTIVE_REPLACEMENT_SCOPE_INVALID');
 const v=value as Record<string,unknown>;
 if(v.tenant_id!==tenant||v.project_id!==project||v.review_id!==review||v.original_task_id!==originalTask||v.decision_hash!==decisionHash)throw new Error('PERSPECTIVE_REPLACEMENT_SCOPE_INVALID');
 const owned=structuredClone({tenant:v.tenant_id,project:v.project_id,review:v.review_id,decisionHash:v.decision_hash,originalRun:v.original_run_id,originalTask:v.original_task_id,run:v.run_id,workflow:v.workflow_id,task:v.task_id,base:v.base}) as PerspectiveReplacementResultScope;
 validatePerspectiveReplacementScope(owned);return owned;
}
/** Caller supplies the current mapped base, never an old intent's source binding.
 * Admission identity does not establish completion or authorize Apply. */
export async function readPerspectiveReplacementResult(api:Pick<OwnerAPI,'get'|'perspectiveProposal'>,scope:PerspectiveReplacementResultScope,signal:AbortSignal):Promise<PerspectiveReplacementObservation> {
 const owned=structuredClone(scope);
 const active=()=>{if(signal.aborted)throw new Error('PERSPECTIVE_RESULT_CANCELLED');};active();
 validatePerspectiveReplacementScope(owned);
 const status=await api.get<{run_id:string;workflow_id:string;state:string}>(`/api/v1/tenants/{t}/v2-workflows/${encodeURIComponent(owned.workflow)}`,signal);active();
 if(status.run_id!==owned.run||status.workflow_id!==owned.workflow)throw new Error('PERSPECTIVE_RESULT_SCOPE_CHANGED');
 if(['failed','cancelled','completed_with_errors'].includes(status.state))return {state:'terminal',workflow_state:status.state as 'failed'|'cancelled'|'completed_with_errors'};
 if(['draft','queued','running','cancelling'].includes(status.state))return {state:'pending',workflow_state:status.state};
 if(!['waiting_review','completed'].includes(status.state))throw new Error('PERSPECTIVE_RESULT_UNAVAILABLE');
 const value=await api.perspectiveProposal(owned.project,owned.task,owned.base,signal);active();
 return {state:'available',proposal:readPerspectiveProposal(value,owned.tenant,owned.project,owned.task,owned.base)};
}
