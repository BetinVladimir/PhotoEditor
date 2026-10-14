import {sha1} from '@noble/hashes/legacy.js';
import type {OwnerAPI} from './owner-api';
import {regionJobTicket,type RegionJobTicket} from './region-preview';
import {freezePerspectiveIntent,type PerspectiveIntent} from './perspective-intent';
import {readPerspectiveProposal,type PerspectiveProposal} from './perspective-proposal';
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
/** Matches Go v2input.TaskID(workflow,"optical.perspective",0). */
export function perspectiveProducerTask(workflow:string):string{
 if(!uuid(workflow))throw new Error('PERSPECTIVE_WORKFLOW_INVALID');
 const namespace=workflow.replaceAll('-',''),name=new TextEncoder().encode('optical.perspective:0'),bytes=new Uint8Array(16+name.length);
 for(let i=0;i<16;i++)bytes[i]=parseInt(namespace.slice(i*2,i*2+2),16);
 bytes.set(name,16);const hash=sha1(bytes).slice(0,16);hash[6]=(hash[6]&15)|80;hash[8]=(hash[8]&63)|128;
 const hex=Array.from(hash,v=>v.toString(16).padStart(2,'0')).join('');return `${hex.slice(0,8)}-${hex.slice(8,12)}-${hex.slice(12,16)}-${hex.slice(16,20)}-${hex.slice(20)}`;
}
export type PerspectiveResultObservation={state:'pending';workflow_state:string}|{state:'available';proposal:PerspectiveProposal};
export async function readPerspectiveAttemptResult(api:Pick<OwnerAPI,'get'|'perspectiveProposal'>,tenant:string,project:string,request:PerspectiveIntent,ticket:RegionJobTicket,signal:AbortSignal):Promise<PerspectiveResultObservation>{
 const body=freezePerspectiveIntent(request),owned=regionJobTicket(ticket);
 const active=()=>{if(signal.aborted)throw new Error('PERSPECTIVE_RESULT_CANCELLED');};active();
 if(!uuid(tenant)||!uuid(project))throw new Error('PERSPECTIVE_RESULT_SCOPE_INVALID');
 const status=await api.get<{run_id:string;workflow_id:string;state:string}>(`/api/v1/tenants/{t}/v2-workflows/${encodeURIComponent(owned.workflow_id)}`,signal);active();
 if(status.run_id!==owned.run_id||status.workflow_id!==owned.workflow_id)throw new Error('PERSPECTIVE_RESULT_SCOPE_CHANGED');
 if(['draft','queued','running'].includes(status.state))return {state:'pending',workflow_state:status.state};
 if(status.state!=='waiting_review'&&status.state!=='completed')throw new Error('PERSPECTIVE_RESULT_UNAVAILABLE');
 const producer=perspectiveProducerTask(owned.workflow_id);
 const value=await api.perspectiveProposal(project,producer,body.params.base,signal);active();
 return {state:'available',proposal:readPerspectiveProposal(value,tenant,project,producer,body.params.base)};
}
