import {semanticWireTime} from './semantic-wire-time';
import type {ApiClient} from './index';
import type {SemanticArtifactRef,SemanticTargetResource} from './semantic';
import type {SemanticReviewStatus} from './semantic-close';

export type SemanticPreviewQuery={tenant_id:string;intent_id:string;target_id:string;target_revision:string;asset_version_id:string;workflow_id:string;workflow_revision:string;plan_sha:string;candidate:SemanticArtifactRef};
export type SemanticPreviewMedia=SemanticArtifactRef&{url:string;expires_at:string;width:number;height:number;media_type:'image/png'|'image/jpeg'|'image/webp'};
export type SemanticReviewPreview=SemanticPreviewQuery&{schema_version:1;render:SemanticArtifactRef;evidence:SemanticArtifactRef;quality_status:'pass'|'review';limitations:string[];protected_pixels_unchanged:true;before:SemanticPreviewMedia;after:SemanticPreviewMedia;affected:SemanticPreviewMedia;protected:SemanticPreviewMedia};
const record=(v:unknown):v is Record<string,unknown>=>v!==null&&typeof v==='object'&&!Array.isArray(v);
const exact=(v:Record<string,unknown>,keys:readonly string[])=>Object.keys(v).length===keys.length&&keys.every(key=>Object.hasOwn(v,key));
const id=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const hash=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
const revision=(v:unknown):v is string=>typeof v==='string'&&/^[1-9][0-9]{0,18}$/.test(v)&&BigInt(v)<9223372036854775807n;
const ref=(v:unknown):v is SemanticArtifactRef=>record(v)&&exact(v,['artifact_id','sha256'])&&id(v.artifact_id)&&hash(v.sha256);
const bindingKeys=['tenant_id','intent_id','target_id','target_revision','asset_version_id','workflow_id','workflow_revision','plan_sha','candidate'] as const;
export function semanticPreviewQuery(tenant:string,intent:string,target:SemanticTargetResource,status:SemanticReviewStatus):SemanticPreviewQuery{
 const query={tenant_id:tenant,intent_id:intent,target_id:target.id,target_revision:target.revision,asset_version_id:target.data.asset_version_id,workflow_id:status.workflow_id,workflow_revision:status.revision,plan_sha:status.plan_sha,candidate:target.document};
 if(!validQuery(query)||target.data.tenant_id!==tenant||target.data.intent_id!==intent||target.data.id!==target.id||target.data.revision!==target.revision||target.data.state!=='proposed'||target.data.candidate_artifact_id!==target.document?.artifact_id||status.state!=='waiting_review')throw new Error('SEMANTIC_PREVIEW_BINDING_INVALID');
 return structuredClone(query) as SemanticPreviewQuery;
}
function validQuery(value:unknown):value is SemanticPreviewQuery{
 return record(value)&&exact(value,bindingKeys)&&['tenant_id','intent_id','target_id','asset_version_id','workflow_id'].every(key=>id(value[key]))&&revision(value.target_revision)&&revision(value.workflow_revision)&&hash(value.plan_sha)&&ref(value.candidate);
}
function media(value:unknown,now:number):value is SemanticPreviewMedia{
 if(!record(value)||!exact(value,['artifact_id','sha256','url','expires_at','width','height','media_type'])||!id(value.artifact_id)||!hash(value.sha256)||typeof value.media_type!=='string'||!['image/png','image/jpeg','image/webp'].includes(value.media_type)||!Number.isInteger(value.width)||!Number.isInteger(value.height)||Number(value.width)<1||Number(value.height)<1||Number(value.width)>4096||Number(value.height)>4096||typeof value.url!=='string'||value.url.length>8192||!semanticWireTime(value.expires_at))return false;
 const expires=Date.parse(value.expires_at);if(!Number.isFinite(expires)||expires<=now||expires>now+300000)return false;
 try{const url=new URL(value.url);return !url.username&&!url.password&&!url.hash&&(url.protocol==='https:'||url.protocol==='http:'&&['localhost','127.0.0.1','[::1]'].includes(url.hostname));}catch{return false;}
}
export function decodeSemanticReviewPreview(value:unknown,query:SemanticPreviewQuery,now=Date.now()):SemanticReviewPreview{
 const fail=():never=>{throw new Error('SEMANTIC_REVIEW_PREVIEW_INVALID')};
 if(!validQuery(query)||!Number.isFinite(now)||!record(value)||!exact(value,[...bindingKeys,'schema_version','render','evidence','quality_status','limitations','protected_pixels_unchanged','before','after','affected','protected'])||value.schema_version!==1)return fail();
 for(const key of bindingKeys){if(key==='candidate'){if(!ref(value.candidate)||value.candidate.artifact_id!==query.candidate.artifact_id||value.candidate.sha256!==query.candidate.sha256)return fail();}else if(value[key]!==query[key])return fail();}
 if(!ref(value.render)||!ref(value.evidence)||new Set([value.render.artifact_id,value.evidence.artifact_id,query.candidate.artifact_id]).size!==3||(value.quality_status!=='pass'&&value.quality_status!=='review')||value.protected_pixels_unchanged!==true||!Array.isArray(value.limitations)||value.limitations.length>20||value.limitations.some(v=>typeof v!=='string'||!v.trim()||[...v].length>500))return fail();
 const images=[value.before,value.after,value.affected,value.protected];
 if(!images.every(image=>media(image,now)))return fail();
 const typed=images as SemanticPreviewMedia[];
 if(typed.some(image=>image.width!==typed[0].width||image.height!==typed[0].height)||new Set([...typed.map(image=>image.artifact_id),value.render.artifact_id,value.evidence.artifact_id,query.candidate.artifact_id]).size!==7||typed[2].media_type!=='image/png'||typed[3].media_type!=='image/png')return fail();
 return structuredClone(value) as SemanticReviewPreview;
}
export function semanticPreviewStillCurrent(value:SemanticReviewPreview|undefined,query:SemanticPreviewQuery,now=Date.now()):boolean{
 try{decodeSemanticReviewPreview(value,query,now);return true;}catch{return false;}
}
export async function readSemanticReviewPreview(client:Pick<ApiClient,'request'>,query:SemanticPreviewQuery,signal?:AbortSignal):Promise<SemanticReviewPreview>{
 if(!validQuery(query))throw new Error('SEMANTIC_PREVIEW_BINDING_INVALID');
 const owned=structuredClone(query),params=new URLSearchParams({expected_revision:owned.workflow_revision,target_revision:owned.target_revision,plan_sha:owned.plan_sha,candidate_sha:owned.candidate.sha256});
 signal?.throwIfAborted();
 const value=await client.request<unknown>(`/api/v1/tenants/${owned.tenant_id}/semantic-intents/${owned.intent_id}/targets/${owned.target_id}/preview?${params}`,{signal});
 signal?.throwIfAborted();
 return decodeSemanticReviewPreview(value,owned);
}

export type SemanticPreviewAdmission={run_id:string;task_id:string;status:'queued'|'running'|'completed'|'failed'|'cancelled'|'cancelling';commit_attempted:boolean;recovered:boolean};
export function decodeSemanticPreviewAdmission(value:unknown):SemanticPreviewAdmission{
 if(!record(value)||!exact(value,['run_id','task_id','status','commit_attempted','recovered'])||!id(value.run_id)||!id(value.task_id)||value.run_id===value.task_id||typeof value.status!=='string'||!['queued','running','completed','failed','cancelled','cancelling'].includes(value.status)||typeof value.commit_attempted!=='boolean'||typeof value.recovered!=='boolean')throw new Error('SEMANTIC_PREVIEW_ADMISSION_INVALID');
 return structuredClone(value) as SemanticPreviewAdmission;
}
export async function admitSemanticReviewPreview(client:Pick<ApiClient,'request'>,query:SemanticPreviewQuery,signal?:AbortSignal):Promise<SemanticPreviewAdmission>{
 if(!validQuery(query))throw new Error('SEMANTIC_PREVIEW_BINDING_INVALID');
 const owned=structuredClone(query),params=new URLSearchParams({expected_revision:owned.workflow_revision,target_revision:owned.target_revision,plan_sha:owned.plan_sha,candidate_sha:owned.candidate.sha256});
 signal?.throwIfAborted();
 const value=await client.request<unknown>(`/api/v1/tenants/${owned.tenant_id}/semantic-intents/${owned.intent_id}/targets/${owned.target_id}/preview?${params}`,{method:'POST',signal});
 signal?.throwIfAborted();
 return decodeSemanticPreviewAdmission(value);
}
