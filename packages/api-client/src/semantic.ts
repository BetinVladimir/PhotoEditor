import {semanticWireTime} from './semantic-wire-time';
import type { ApiClient } from './index';

export type SemanticTargetState='proposed'|'queued'|'rejected'|'unsupported'|'applied'|'failed'|'cancelled';
export type SemanticArtifactRef={artifact_id:string;sha256:string};
export type SemanticTargetRow={resource_type:'semantic_targets';id:string;tenant_id:string;project_id:string;created_at:string;revision:string;updated_at:string;intent_id:string;child_run_id:string|null;batch_id:string|null;asset_version_id:string;candidate_artifact_id:string|null;review_id:string|null;override_id:string|null;state:SemanticTargetState};
export type SemanticTargetResource={id:string;revision:string;document:SemanticArtifactRef|null;data:SemanticTargetRow};
export type SemanticTargetPage={items:SemanticTargetResource[];next_cursor:string|null};
export type SemanticTargetQuery={limit?:number;cursor?:string;signal?:AbortSignal};
const uuid=/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const validID=(v:unknown):v is string=>typeof v==='string'&&uuid.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
// Target revisions are full positive int64 decimal strings, independent of parent revisions.
const revision=(v:unknown):v is string=>typeof v==='string'&&/^[1-9][0-9]{0,18}$/.test(v)&&(v.length<19||v<='9223372036854775807');
const record=(v:unknown):v is Record<string,unknown>=>v!==null&&typeof v==='object'&&!Array.isArray(v);
const exact=(v:Record<string,unknown>,keys:string[])=>Object.keys(v).length===keys.length&&keys.every(k=>Object.hasOwn(v,k));
const states=new Set<string>(['proposed','queued','rejected','unsupported','applied','failed','cancelled']);
const rowKeys=['resource_type','id','tenant_id','project_id','created_at','revision','updated_at','intent_id','child_run_id','batch_id','asset_version_id','candidate_artifact_id','review_id','override_id','state'];
const timestamp=semanticWireTime;

export function decodeSemanticTargets(value:unknown,tenant:string,intent:string):SemanticTargetPage{
 const fail=():never=>{throw new Error('SEMANTIC_TARGET_RESPONSE_INVALID')};
 if(!validID(tenant)||!validID(intent)||!record(value)||!exact(value,['items','next_cursor'])||!Array.isArray(value.items)||value.items.length>100||(value.next_cursor!==null&&(typeof value.next_cursor!=='string'||value.next_cursor.length<1||value.next_cursor.length>2048)))return fail();
 const seen=new Set<string>();
 for(const item of value.items){
  if(!record(item)||!exact(item,['id','revision','document','data'])||!validID(item.id)||!revision(item.revision)||seen.has(item.id)||!record(item.data)||!exact(item.data,rowKeys))return fail();
  seen.add(item.id);const row=item.data;
  if(row.resource_type!=='semantic_targets'||row.id!==item.id||row.revision!==item.revision||row.tenant_id!==tenant||row.intent_id!==intent||!validID(row.project_id)||!validID(row.asset_version_id)||!timestamp(row.created_at)||!timestamp(row.updated_at)||typeof row.state!=='string'||!states.has(row.state))return fail();
  for(const key of ['child_run_id','batch_id','candidate_artifact_id','review_id','override_id'])if(row[key]!==null&&!validID(row[key]))return fail();
  if(item.document!==null){const ref=item.document;if(!record(ref)||!exact(ref,['artifact_id','sha256'])||!validID(ref.artifact_id)||ref.artifact_id!==row.candidate_artifact_id||typeof ref.sha256!=='string'||!/^[0-9a-f]{64}$/.test(ref.sha256))return fail()}
 }
 return structuredClone(value) as SemanticTargetPage;
}

export async function listSemanticTargets(client:Pick<ApiClient,'request'>,tenant:string,intent:string,query:SemanticTargetQuery={}):Promise<SemanticTargetPage>{
 if(!validID(tenant)||!validID(intent)||query.limit!==undefined&&(!Number.isInteger(query.limit)||query.limit<1||query.limit>100)||query.cursor!==undefined&&(typeof query.cursor!=='string'||query.cursor.length<1||query.cursor.length>2048))throw new Error('SEMANTIC_TARGET_QUERY_INVALID');
 const params=new URLSearchParams();if(query.limit!==undefined)params.set('limit',String(query.limit));if(query.cursor!==undefined)params.set('cursor',query.cursor);
 const suffix=params.size?`?${params}`:'';
 const signal=query.signal;
 signal?.throwIfAborted();
 const value=await client.request<unknown>(`/api/v1/tenants/${tenant}/semantic-intents/${intent}/targets${suffix}`,{signal});
 signal?.throwIfAborted();
 return decodeSemanticTargets(value,tenant,intent);
}
