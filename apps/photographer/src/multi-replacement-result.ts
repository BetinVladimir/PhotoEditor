import type {OwnerAPI} from './owner-api';
import {freezeMultiIntent,multiUUID,type MultiIntent,type MultiOperation} from './multi-intent';
import {multiNamespacedUUID,multiWorkflowID,type MultiResultObservation} from './multi-result';
import {readMultiProposal} from './multi-proposal';

export type MultiReplacementResultScope={tenant:string;project:string;review:string;decisionHash:string;originalRun:string;originalTask:string;run:string;workflow:string;task:string;operation:MultiOperation;intent:MultiIntent};
/** Replacement IDs use the invalidation namespace, never the ordinary producer ID. */
export function freezeMultiReplacementScope(scope:MultiReplacementResultScope):MultiReplacementResultScope{
 const owned=structuredClone(scope);
 if(![owned.tenant,owned.project,owned.review,owned.originalRun,owned.originalTask,owned.run,owned.workflow,owned.task].every(multiUUID)||!/^[0-9a-f]{64}$/.test(owned.decisionHash))throw new Error('MULTI_REPLACEMENT_SCOPE_INVALID');
 owned.intent=freezeMultiIntent(owned.operation,owned.intent);
 const namespace=multiNamespacedUUID(owned.tenant,`V29.NegativeReplacement/v2:${owned.project}:${owned.review}:${owned.decisionHash}`);
 if(owned.workflow!==multiWorkflowID(owned.run)||owned.run!==multiNamespacedUUID(namespace,`run:${owned.originalRun}:${owned.originalTask}`)||owned.task!==multiNamespacedUUID(namespace,`task:${owned.originalTask}`))throw new Error('MULTI_REPLACEMENT_SCOPE_INVALID');
 return owned;
}
export function readMultiReplacementScope(value:unknown,tenant:string,project:string,review:string,originalTask:string,decisionHash:string,operation:MultiOperation):MultiReplacementResultScope{
 const keys=['tenant_id','project_id','review_id','decision_hash','original_run_id','original_task_id','run_id','workflow_id','task_id','operation','intent'];
 if(!value||typeof value!=='object'||Array.isArray(value)||Object.keys(value).length!==keys.length||!keys.every(k=>Object.prototype.hasOwnProperty.call(value,k)))throw new Error('MULTI_REPLACEMENT_SCOPE_INVALID');
 const v=value as Record<string,unknown>;
 if(v.tenant_id!==tenant||v.project_id!==project||v.review_id!==review||v.original_task_id!==originalTask||v.decision_hash!==decisionHash||v.operation!==operation)throw new Error('MULTI_REPLACEMENT_SCOPE_INVALID');
 return freezeMultiReplacementScope({tenant,project,review,decisionHash,originalTask,originalRun:v.original_run_id,run:v.run_id,workflow:v.workflow_id,task:v.task_id,operation,intent:v.intent} as MultiReplacementResultScope);
}
/** Scope must come from current server discovery with mapped frames. Old intent
 * and an admission receipt alone cannot establish a reviewable result. */
export async function readMultiReplacementResult(api:Pick<OwnerAPI,'get'|'multiProposal'>,scope:MultiReplacementResultScope,signal:AbortSignal):Promise<MultiResultObservation>{
 const active=()=>{if(signal.aborted)throw new Error('MULTI_RESULT_CANCELLED');};active();
 const owned=freezeMultiReplacementScope(scope);
 const status=await api.get<unknown>(`/api/v1/tenants/{t}/v2-workflows/${encodeURIComponent(owned.workflow)}`,signal);active();
 if(!status||typeof status!=='object'||Array.isArray(status))throw new Error('MULTI_RESULT_SCOPE_CHANGED');
 const current=status as Record<string,unknown>;
 if(current.run_id!==owned.run||current.workflow_id!==owned.workflow)throw new Error('MULTI_RESULT_SCOPE_CHANGED');
 if(current.state==='draft'||current.state==='queued'||current.state==='running'||current.state==='cancelling')return {state:'pending',workflow_state:current.state};
 if(current.state==='failed'||current.state==='cancelled')return {state:'terminal',workflow_state:current.state};
 if(current.state!=='waiting_review'&&current.state!=='completed')throw new Error('MULTI_RESULT_UNAVAILABLE');
 const value=await api.multiProposal(owned.operation,owned.project,owned.task,owned.intent,signal);active();
 return {state:'available',proposal:readMultiProposal(value,owned.tenant,owned.project,owned.task,owned.operation,owned.intent)};
}
