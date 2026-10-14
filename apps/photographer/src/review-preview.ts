import type {Review,RunOutput} from './catalog-types';
import {canonicalJSON} from '@photoeditor/api-client';
import {ownRenderReviewBinding,hasBoundCandidatePreview} from './review-binding';
import {bindReviewPreview} from './review-preview-manifest';
import {readRegionArtifact} from './region-preview-download';

type Side='before'|'after';
type Envelope={source:{artifact_id:string;sha256:string};profile:string;width:number;height:number;manifest:RunOutput;image:RunOutput};
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const sha=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
function exact(v:unknown,keys:string[]):v is Record<string,unknown>{return !!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.hasOwn(v,k));}
function ref(v:unknown):v is {artifact_id:string;sha256:string}{return exact(v,['artifact_id','sha256'])&&uuid(v.artifact_id)&&sha(v.sha256)}
export function readReviewPreview(value:unknown,review:Review,side:Side):Envelope{
 const owned=ownRenderReviewBinding(review),fail=():never=>{throw new Error('REVIEW_PREVIEW_INVALID')};
 if(!hasBoundCandidatePreview(owned.kind)||!exact(value,['review_id','side','source','profile_hash','width','height','manifest','image'])||value.review_id!==owned.id||value.side!==side||!ref(value.source)||canonicalJSON(value.source)!==canonicalJSON(side==='before'?owned.base:owned.candidate)||!sha(value.profile_hash)||typeof value.width!=='number'||typeof value.height!=='number'||!Number.isSafeInteger(value.width)||!Number.isSafeInteger(value.height)||value.width<1||value.height<1||Math.max(value.width,value.height)>2560)return fail();
 const link=(v:unknown,role:string):RunOutput=>{
  if(!exact(v,['artifact','bytes','media_type','url','expires_at'])||!ref(v.artifact)||typeof v.bytes!=='number'||!Number.isSafeInteger(v.bytes)||v.bytes<2||typeof v.media_type!=='string'||typeof v.url!=='string'||typeof v.expires_at!=='string')return fail();
  return {artifact_id:v.artifact.artifact_id,sha256:v.artifact.sha256,role,bytes:v.bytes,media_type:v.media_type,url:v.url,expires_at:v.expires_at};
 };
 return {source:{...value.source},profile:value.profile_hash,width:value.width,height:value.height,manifest:link(value.manifest,'derivative_manifest'),image:link(value.image,'derivative')};
}
export type ReviewPreviewBytes={profile:string;source:Envelope['source'];width:number;height:number;image:RunOutput;bytes:Uint8Array};
export async function loadReviewPreview(api:{reviewPreview:(id:string,side:Side,signal:AbortSignal)=>Promise<unknown>},review:Review,side:Side,signal:AbortSignal,transport:typeof fetch=fetch):Promise<ReviewPreviewBytes>{
 const owned=ownRenderReviewBinding(review),active=()=>{if(signal.aborted)throw new Error('REVIEW_PREVIEW_ABORTED')};active();
 const envelope=readReviewPreview(await api.reviewPreview(owned.id,side,signal),owned,side);active();
 const body=await readRegionArtifact(envelope.manifest,'review_manifest',signal,transport);active();
 const bound=bindReviewPreview(body,envelope.manifest,envelope.image,owned,side,envelope.profile);
 if(bound.width!==envelope.width||bound.height!==envelope.height)throw new Error('REVIEW_PREVIEW_INVALID');
 const bytes=await readRegionArtifact(envelope.image,'review_derivative',signal,transport);active();
 return {profile:envelope.profile,source:bound.source,width:bound.width,height:bound.height,image:bound.image,bytes};
}
