import {sha1} from '@noble/hashes/legacy.js';
import type {OwnerAPI} from './owner-api';
import {freezeMultiIntent,multiUUID,type MultiIntent,type MultiOperation} from './multi-intent';
import {regionJobTicket,type RegionJobTicket} from './region-preview';
import {readMultiProposal,type MultiProposal} from './multi-proposal';
export function multiNamespacedUUID(namespace:string,name:string){
 if(!multiUUID(namespace))throw new Error('MULTI_RESULT_SCOPE_INVALID');
 const hex=namespace.replaceAll('-',''),text=new TextEncoder().encode(name),bytes=new Uint8Array(16+text.length);
 for(let i=0;i<16;i++)bytes[i]=parseInt(hex.slice(i*2,i*2+2),16);bytes.set(text,16);
 const digest=sha1(bytes).slice(0,16);digest[6]=(digest[6]&15)|80;digest[8]=(digest[8]&63)|128;
 const value=Array.from(digest,n=>n.toString(16).padStart(2,'0')).join('');return `${value.slice(0,8)}-${value.slice(8,12)}-${value.slice(12,16)}-${value.slice(16,20)}-${value.slice(20)}`;
}
export function multiWorkflowID(run:string){return multiNamespacedUUID(run,'workflow');}
export function multiProducerTask(workflow:string,operation:MultiOperation){
 if(operation!=='optical.panorama'&&operation!=='optical.focus_stack')throw new Error('MULTI_RESULT_OPERATION_INVALID');
 return multiNamespacedUUID(workflow,operation+':0');
}
/** Derive a journal lookup scope from an acknowledged ticket without result/network IO. */
export function multiTicketProducer(operation:MultiOperation,ticket:RegionJobTicket){
 const owned=regionJobTicket(ticket);
 if(multiWorkflowID(owned.run_id)!==owned.workflow_id)throw new Error('MULTI_RESULT_SCOPE_INVALID');
 return multiProducerTask(owned.workflow_id,operation);
}
export type MultiResultObservation={state:'pending';workflow_state:'draft'|'queued'|'running'|'cancelling'}|{state:'terminal';workflow_state:'failed'|'cancelled'|'completed_with_errors'}|{state:'available';proposal:MultiProposal};
/** A durable ticket is read-only recovery: never resubmit or change frame order. */
export async function readMultiAttemptResult(api:Pick<OwnerAPI,'get'|'multiProposal'>,tenant:string,project:string,operation:MultiOperation,request:MultiIntent,ticket:RegionJobTicket,signal:AbortSignal):Promise<MultiResultObservation>{
 const active=()=>{if(signal.aborted)throw new Error('MULTI_RESULT_CANCELLED');};active();
 const body=freezeMultiIntent(operation,request),owned=regionJobTicket(ticket);
 if(!multiUUID(tenant)||!multiUUID(project)||multiWorkflowID(owned.run_id)!==owned.workflow_id)throw new Error('MULTI_RESULT_SCOPE_INVALID');
 const status=await api.get<unknown>(`/api/v1/tenants/{t}/v2-workflows/${encodeURIComponent(owned.workflow_id)}`,signal);active();
 if(!status||typeof status!=='object'||Array.isArray(status))throw new Error('MULTI_RESULT_SCOPE_CHANGED');
 const current=status as Record<string,unknown>;
 if(current.run_id!==owned.run_id||current.workflow_id!==owned.workflow_id)throw new Error('MULTI_RESULT_SCOPE_CHANGED');
 if(current.state==='draft'||current.state==='queued'||current.state==='running'||current.state==='cancelling')return {state:'pending',workflow_state:current.state};
 if(current.state==='failed'||current.state==='cancelled'||current.state==='completed_with_errors')return {state:'terminal',workflow_state:current.state};
 if(current.state!=='waiting_review'&&current.state!=='completed')throw new Error('MULTI_RESULT_UNAVAILABLE');
 const producer=multiTicketProducer(operation,owned);
 const value=await api.multiProposal(operation,project,producer,body,signal);active();
 return {state:'available',proposal:readMultiProposal(value,tenant,project,producer,operation,body)};
}
