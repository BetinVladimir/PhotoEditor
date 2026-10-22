import type {ApiClient} from './index';
export type SemanticReviewClose={expected_revision:string;plan_sha:string};
export type SemanticReviewClosed={run_id:string;workflow_id:string;revision:string;state:'completed';outputs:[];error_code:null};
const id=(value:unknown):value is string=>typeof value==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(value)&&value!=='00000000-0000-0000-0000-000000000000';
const revision=(value:unknown):value is string=>typeof value==='string'&&/^[1-9][0-9]{0,15}$/.test(value)&&BigInt(value)<=9007199254740991n;
const record=(value:unknown):value is Record<string,unknown>=>value!==null&&typeof value==='object'&&!Array.isArray(value);
export function decodeSemanticReviewClosed(value:unknown):SemanticReviewClosed {
 if(!record(value)||Object.keys(value).length!==6||!['run_id','workflow_id','revision','state','outputs','error_code'].every(key=>Object.hasOwn(value,key))||!id(value.run_id)||!id(value.workflow_id)||!revision(value.revision)||BigInt(value.revision)<2n||value.state!=='completed'||!Array.isArray(value.outputs)||value.outputs.length!==0||value.error_code!==null)throw new Error('SEMANTIC_CLOSE_RESPONSE_INVALID');
 return structuredClone(value) as SemanticReviewClosed;
}
// Keep one idempotency key for retries of the same immutable revision/plan.
export async function closeSemanticReview(client:Pick<ApiClient,'request'>,tenant:string,intent:string,request:SemanticReviewClose,key:string,signal?:AbortSignal):Promise<SemanticReviewClosed> {
 if(!id(tenant)||!id(intent)||!record(request)||Object.keys(request).length!==2||!revision(request.expected_revision)||BigInt(request.expected_revision)>=9007199254740991n||typeof request.plan_sha!=='string'||!/^[0-9a-f]{64}$/.test(request.plan_sha)||typeof key!=='string'||!/^[!-~]{1,128}$/.test(key))throw new Error('SEMANTIC_CLOSE_REQUEST_INVALID');
 const body={expected_revision:request.expected_revision,plan_sha:request.plan_sha};
 signal?.throwIfAborted();
 const raw=await client.request<unknown>(`/api/v1/tenants/${tenant}/semantic-intents/${intent}/close-review`,{method:'POST',body,idempotencyKey:key,signal});
 signal?.throwIfAborted();
 const result=decodeSemanticReviewClosed(raw);
 if(result.revision!==(BigInt(body.expected_revision)+1n).toString())throw new Error('SEMANTIC_CLOSE_RESPONSE_INVALID');
 return result;
}

export type SemanticReviewStatus={kind?:'remove_object'|'local_exposure'|'background_cleanup'|'retouch_region';run_id:string;workflow_id:string;revision:string;state:'queued'|'running'|'waiting_review'|'cancelling'|'cancelled'|'completed'|'failed';plan_sha:string|null;remaining:number;can_close:boolean};
export function decodeSemanticReviewStatus(value:unknown):SemanticReviewStatus {
 if(!record(value)||(Object.keys(value).length!==(Object.hasOwn(value,'kind')?8:7)||Object.hasOwn(value,'kind')&&(typeof value.kind!=='string'||!['remove_object','local_exposure','background_cleanup','retouch_region'].includes(value.kind)))||!['run_id','workflow_id','revision','state','plan_sha','remaining','can_close'].every(key=>Object.hasOwn(value,key))||!id(value.run_id)||!id(value.workflow_id)||!revision(value.revision)||typeof value.state!=='string'||!['queued','running','waiting_review','cancelling','cancelled','completed','failed'].includes(value.state)||!(value.plan_sha===null||typeof value.plan_sha==='string'&&/^[0-9a-f]{64}$/.test(value.plan_sha))||!Number.isInteger(value.remaining)||Number(value.remaining)<0||Number(value.remaining)>5000||typeof value.can_close!=='boolean'||value.can_close&&(value.state!=='waiting_review'||value.remaining!==0||value.plan_sha===null||BigInt(value.revision)>=9007199254740991n))throw new Error('SEMANTIC_REVIEW_STATUS_INVALID');
 return structuredClone(value) as SemanticReviewStatus;
}
export async function readSemanticReviewStatus(client:Pick<ApiClient,'request'>,tenant:string,intent:string,signal?:AbortSignal):Promise<SemanticReviewStatus> {
 if(!id(tenant)||!id(intent))throw new Error('SEMANTIC_REVIEW_QUERY_INVALID');
 signal?.throwIfAborted();
 const raw=await client.request<unknown>(`/api/v1/tenants/${tenant}/semantic-intents/${intent}/review`,{signal});
 signal?.throwIfAborted();
 const result=decodeSemanticReviewStatus(raw);
 return result;
}

export type SemanticRunIntent={run_id:string;workflow_id:string;intent_id:string|null;revision:string;state:SemanticReviewStatus['state']};
export function decodeSemanticRunIntent(value:unknown):SemanticRunIntent {
 if(!record(value)||Object.keys(value).length!==5||!['run_id','workflow_id','intent_id','revision','state'].every(key=>Object.hasOwn(value,key))||!id(value.run_id)||!id(value.workflow_id)||!(value.intent_id===null||id(value.intent_id))||!revision(value.revision)||typeof value.state!=='string'||!['queued','running','waiting_review','cancelling','cancelled','completed','failed'].includes(value.state))throw new Error('SEMANTIC_RUN_INTENT_INVALID');
 return structuredClone(value) as SemanticRunIntent;
}
export async function readSemanticRunIntent(client:Pick<ApiClient,'request'>,tenant:string,run:string,signal?:AbortSignal):Promise<SemanticRunIntent> {
 if(!id(tenant)||!id(run))throw new Error('SEMANTIC_RUN_QUERY_INVALID');
 signal?.throwIfAborted();
 const raw=await client.request<unknown>(`/api/v1/tenants/${tenant}/runs/${run}/semantic-intent`,{signal});
 signal?.throwIfAborted();
 const result=decodeSemanticRunIntent(raw);
 if(result.run_id!==run)throw new Error('SEMANTIC_RUN_INTENT_INVALID');
 return result;
}
