import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import type {Review,RunOutput} from './catalog-types';
import {ownRenderReviewBinding,hasBoundCandidatePreview} from './review-binding';

type Ref={artifact_id:string;sha256:string};
const hash=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
function exact(v:unknown,keys:string[]):v is Record<string,unknown>{return !!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.hasOwn(v,k));}
function reference(v:unknown):v is Ref{return exact(v,['artifact_id','sha256'])&&typeof v.artifact_id==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v.artifact_id)&&v.artifact_id!=='00000000-0000-0000-0000-000000000000'&&hash(v.sha256);}

/** Original media.preview manifest bytes, pinned to one exact review side.
 * expectedProfile must come from the installed qualified presentation policy. */
export function bindReviewPreview(body:Uint8Array,document:RunOutput,image:RunOutput,review:Review,side:'before'|'after',expectedProfile:string,now=Date.now()):{image:RunOutput;width:number;height:number;source:Ref}{
 const fail=():never=>{throw new Error('REVIEW_PREVIEW_INVALID')};
 const owned=ownRenderReviewBinding(review);
 if(!hasBoundCandidatePreview(owned.kind)||!hash(expectedProfile)||!Number.isFinite(now)||body.length<2||body.length>1048576||document.role!=='derivative_manifest'||document.media_type!=='application/json'||document.bytes!==body.length||bytesToHex(sha256(body))!==document.sha256)return fail();
 let raw:string,value:unknown;try{raw=new TextDecoder('utf-8',{fatal:true}).decode(body);value=JSON.parse(raw);if(canonicalJSON(value)!==raw)return fail()}catch{return fail()}
 if(!exact(value,['source','derivative','variant','profile_hash','width','height','media_type','color_profile','metadata_policy','alpha'])||!reference(value.source)||!reference(value.derivative))return fail();
 const source=side==='before'?owned.base!:owned.candidate!;
 const edge=value.variant==='preview1600'?1600:value.variant==='preview2560'?2560:0;
 if(!edge||canonicalJSON(value.source)!==canonicalJSON(source)||value.profile_hash!==expectedProfile||value.color_profile!=='srgb'||value.metadata_policy!=='stripped'||typeof value.alpha!=='boolean'||value.media_type!==(value.alpha?'image/webp':'image/jpeg')||image.role!=='derivative'||image.media_type!==value.media_type||value.derivative.artifact_id!==image.artifact_id||value.derivative.sha256!==image.sha256||image.artifact_id===source.artifact_id||!Number.isSafeInteger(image.bytes)||image.bytes<2||image.bytes>52*1048576||typeof value.width!=='number'||typeof value.height!=='number'||!Number.isSafeInteger(value.width)||!Number.isSafeInteger(value.height)||value.width<1||value.height<1||Math.max(value.width,value.height)>edge)return fail();
 for(const output of [document,image]){let url:URL;try{url=new URL(output.url)}catch{return fail()};const versions=url.searchParams.getAll('versionId');if(!['http:','https:'].includes(url.protocol)||!url.hostname||url.username||url.password||url.hash||versions.length!==1||!versions[0]||versions[0]==='null'||url.protocol==='http:'&&!['localhost','[::1]'].includes(url.hostname)&&!/^127(?:\.[0-9]{1,3}){3}$/.test(url.hostname)||!Number.isFinite(Date.parse(output.expires_at))||Date.parse(output.expires_at)<=now)return fail()}
 return {image:{...image},width:value.width,height:value.height,source:{...source}};
}
