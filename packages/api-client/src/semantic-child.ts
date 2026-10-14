import type {ApiClient} from './index';
export type SemanticChildState='draft'|'queued'|'running'|'waiting_review'|'cancelling'|'cancelled'|'completed'|'completed_with_errors'|'failed';
export type SemanticChildStatus={id:string;kind:'apply';status:SemanticChildState;revision:string;counts:{total:number;succeeded:number;failed:number;skipped:number;review:number};stage:SemanticChildState;progress:number;errors:string[];output_collection_id?:string};
const record=(v:unknown):v is Record<string,unknown>=>v!==null&&typeof v==='object'&&!Array.isArray(v);
const id=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const validCounts=(v:unknown):v is Record<string,number>=>record(v)&&Object.keys(v).length===5&&['total','succeeded','failed','skipped','review'].every(k=>Number.isSafeInteger(v[k])&&Number(v[k])>=0);
export function decodeSemanticChildStatus(value:unknown,run:string):SemanticChildStatus {
 const fail=():never=>{throw new Error('SEMANTIC_CHILD_STATUS_INVALID')};
 const keys=['id','kind','status','revision','counts','stage','progress','errors'];
 if(!id(run)||!record(value)||!keys.every(k=>Object.hasOwn(value,k))||Object.keys(value).some(k=>!keys.includes(k)&&k!=='output_collection_id')||value.id!==run||value.kind!=='apply'||typeof value.status!=='string'||!['draft','queued','running','waiting_review','cancelling','cancelled','completed','completed_with_errors','failed'].includes(value.status)||value.stage!==value.status||typeof value.revision!=='string'||!/^[1-9][0-9]{0,18}$/.test(value.revision)||BigInt(value.revision)>9223372036854775807n||!validCounts(value.counts)||typeof value.progress!=='number'||!Number.isFinite(value.progress)||value.progress<0||value.progress>1||!Array.isArray(value.errors)||!value.errors.every(e=>typeof e==='string')||Object.hasOwn(value,'output_collection_id')&&!id(value.output_collection_id))return fail();
 const counts=value.counts;
 const terminal=BigInt(counts.succeeded as number)+BigInt(counts.failed as number)+BigInt(counts.skipped as number);
 if(terminal>BigInt(counts.total as number))return fail();
 const expected=Number(counts.total)===0?(['completed','completed_with_errors','failed','cancelled'].includes(value.status)?1:0):Number(terminal)/Number(counts.total);
 if(Math.abs(value.progress-expected)>1e-9)return fail();
 return structuredClone(value) as SemanticChildStatus;
}
export async function readSemanticChildStatus(client:Pick<ApiClient,'request'>,tenant:string,run:string,signal?:AbortSignal):Promise<SemanticChildStatus>{
 if(!id(tenant)||!id(run))throw new Error('SEMANTIC_CHILD_QUERY_INVALID');
 return decodeSemanticChildStatus(await client.request<unknown>(`/api/v1/tenants/${tenant}/runs/${run}`,{signal}),run);
}
