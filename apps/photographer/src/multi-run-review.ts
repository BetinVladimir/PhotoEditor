import {freezeMultiIntent,multiUUID,type MultiIntent,type MultiOperation} from './multi-intent';
import {multiWorkflowID,multiProducerTask} from './multi-result';
export type MultiRunReviewScope={tenant_id:string;project_id:string;reference_asset_id:string;run_id:string;workflow_id:string;task_id:string;operation:MultiOperation;intent:MultiIntent};
export function readMultiRunReview(value:unknown,tenant:string,run:string):MultiRunReviewScope|null{
 if(!value||typeof value!=='object'||Array.isArray(value)||Object.keys(value).join(',')!=='scope')throw new Error('MULTI_RUN_SCOPE_INVALID');
 const scope=(value as {scope:unknown}).scope;if(scope===null)return null;
 if(!scope||typeof scope!=='object'||Array.isArray(scope))throw new Error('MULTI_RUN_SCOPE_INVALID');
 const v=scope as MultiRunReviewScope;
 if(Object.keys(v).sort().join(',')!=='intent,operation,project_id,reference_asset_id,run_id,task_id,tenant_id,workflow_id'||v.tenant_id!==tenant||v.run_id!==run||![tenant,run,v.project_id,v.reference_asset_id,v.workflow_id,v.task_id].every(multiUUID)||v.workflow_id!==multiWorkflowID(run)||v.task_id!==multiProducerTask(v.workflow_id,v.operation))throw new Error('MULTI_RUN_SCOPE_INVALID');
 return {...structuredClone(v),intent:freezeMultiIntent(v.operation,v.intent)};
}
