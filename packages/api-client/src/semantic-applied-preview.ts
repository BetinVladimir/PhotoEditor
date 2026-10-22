import {semanticWireTime} from './semantic-wire-time';
import type {ApiClient} from './index';
export type SemanticAppliedPreview={schema_version:1;tenant_id:string;target_id:string;child_run_id:string;working_image:{artifact_id:string;sha256:string};after:{artifact_id:string;sha256:string;url:string;expires_at:string;width:number;height:number;media_type:'image/png'}};
const id=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const sha=(v:unknown)=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
const record=(v:unknown):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v);
const exact=(v:Record<string,unknown>,keys:string[])=>Object.keys(v).length===keys.length&&keys.every(k=>Object.hasOwn(v,k));
export function decodeSemanticAppliedPreview(value:unknown,tenant:string,target:string,now=Date.now()):SemanticAppliedPreview{
 const fail=():never=>{throw new Error('SEMANTIC_APPLIED_PREVIEW_RESPONSE_INVALID')};
 if(!record(value)||!exact(value,['schema_version','tenant_id','target_id','child_run_id','working_image','after'])||value.schema_version!==1||value.tenant_id!==tenant||value.target_id!==target||!id(value.child_run_id))return fail();
 const image=value.working_image,media=value.after;
 if(!record(image)||!exact(image,['artifact_id','sha256'])||!id(image.artifact_id)||!sha(image.sha256)||!record(media)||!exact(media,['artifact_id','sha256','url','expires_at','width','height','media_type'])||!id(media.artifact_id)||!sha(media.sha256)||media.media_type!=='image/png'||!Number.isInteger(media.width)||!Number.isInteger(media.height)||Number(media.width)<1||Number(media.height)<1||Number(media.width)>2560||Number(media.height)>2560||typeof media.url!=='string'||media.url.length>8192||!semanticWireTime(media.expires_at)||Date.parse(media.expires_at)<=now||Date.parse(media.expires_at)>now+61000)return fail();
 let url:URL;try{url=new URL(media.url)}catch{return fail()}
 if(url.username||url.password||url.hash||!(url.protocol==='https:'||url.protocol==='http:'&&['localhost','127.0.0.1','[::1]'].includes(url.hostname)))return fail();
 return structuredClone(value) as SemanticAppliedPreview;
}
export async function readSemanticAppliedPreview(client:Pick<ApiClient,'request'>,tenant:string,target:string,signal?:AbortSignal):Promise<SemanticAppliedPreview>{
 if(!id(tenant)||!id(target))throw new Error('SEMANTIC_APPLIED_PREVIEW_REQUEST_INVALID');
 signal?.throwIfAborted();
 const raw=await client.request<unknown>(`/api/v1/tenants/${tenant}/semantic-targets/${target}/preview`,{signal});
 if(signal?.aborted)throw new DOMException('Aborted','AbortError');
 return decodeSemanticAppliedPreview(raw,tenant,target);
}
