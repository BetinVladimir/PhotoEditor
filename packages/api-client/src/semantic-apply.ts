import type {ApiClient} from './index';
export type SemanticTargetDecision={target_id:string;decision:'approve'|'reject'};
export type SemanticApplyRequest={expected_revision:string;plan_sha:string;target_decisions:SemanticTargetDecision[]};
export type SemanticApplyResult={workflow_id:string;revision:string;remaining:number;batch_id:string;child:{run_id:string;state:'queued'}|null};
const record=(v:unknown):v is Record<string,unknown>=>v!==null&&typeof v==='object'&&!Array.isArray(v);
const exact=(v:Record<string,unknown>,keys:string[])=>Object.keys(v).length===keys.length&&keys.every(k=>Object.hasOwn(v,k));
const id=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const revision=(v:unknown):v is string=>typeof v==='string'&&/^[1-9][0-9]{0,18}$/.test(v)&&BigInt(v)<=9223372036854775807n;
export function decodeSemanticApplyResult(value:unknown):SemanticApplyResult {
 if(!record(value)||!exact(value,['workflow_id','revision','remaining','batch_id','child'])||!id(value.workflow_id)||!id(value.batch_id)||!revision(value.revision)||!Number.isInteger(value.remaining)||Number(value.remaining)<0||Number(value.remaining)>5000||!(value.child===null||record(value.child)&&exact(value.child,['run_id','state'])&&id(value.child.run_id)&&value.child.state==='queued'))throw new Error('SEMANTIC_APPLY_RESPONSE_INVALID');
 return structuredClone(value) as SemanticApplyResult;
}
export async function applySemanticReview(client:Pick<ApiClient,'request'>,tenant:string,intent:string,input:SemanticApplyRequest,key:string,signal?:AbortSignal):Promise<SemanticApplyResult> {
 const fail=():never=>{throw new Error('SEMANTIC_APPLY_REQUEST_INVALID')};
 if(!id(tenant)||!id(intent)||!record(input)||!exact(input,['expected_revision','plan_sha','target_decisions'])||!revision(input.expected_revision)||BigInt(input.expected_revision)>=9223372036854775807n||typeof input.plan_sha!=='string'||!/^[0-9a-f]{64}$/.test(input.plan_sha)||!Array.isArray(input.target_decisions)||input.target_decisions.length<1||input.target_decisions.length>100||typeof key!=='string'||!/^[!-~]{1,128}$/.test(key))return fail();
 const seen=new Set<string>();
 for(const item of input.target_decisions){if(!record(item)||!exact(item,['target_id','decision'])||!id(item.target_id)||seen.has(item.target_id)||!['approve','reject'].includes(item.decision))return fail();seen.add(item.target_id)}
 const body=structuredClone(input);
 const approved=body.target_decisions.some(item=>item.decision==='approve');
 const result=decodeSemanticApplyResult(await client.request<unknown>(`/api/v1/tenants/${tenant}/semantic-intents/${intent}/apply`,{method:'POST',body,idempotencyKey:key,signal}));
 if(result.revision!==(BigInt(body.expected_revision)+1n).toString()||approved!==(result.child!==null))throw new Error('SEMANTIC_APPLY_RESPONSE_INVALID');
 return result;
}
