import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import type {MediaLink} from './catalog-types';
import type {CompareFrame} from './compare-frames';
const hash=(value:unknown):value is string=>typeof value==='string'&&/^[0-9a-f]{64}$/.test(value);
/** API-authorized derivative metadata; pixel decoding remains the display layer's responsibility. */
export function comparePreview(link:MediaLink,versionId:string,sourceSha:string,now=Date.now()):CompareFrame {
 const edge=link.variant==='preview1600'?1600:link.variant==='preview2560'?2560:0;
 if(!edge||!versionId||link.asset_version_id!==versionId||!hash(sourceSha)||!hash(link.artifact_sha256)||!hash(link.icc_sha256)||!hash(link.profile_hash)||!Number.isFinite(now)||!Number.isFinite(Date.parse(link.expires_at))||Date.parse(link.expires_at)<=now||!Number.isSafeInteger(link.width)||!Number.isSafeInteger(link.height)||link.width<1||link.height<1||Math.max(link.width,link.height)>edge||!['image/jpeg','image/webp'].includes(link.media_type))throw new Error('COMPARE_PREVIEW_INVALID');
 const expected=bytesToHex(sha256(new TextEncoder().encode(canonicalJSON({schema_version:1,variant:link.variant,long_edge:edge,quality:88,icc_sha256:link.icc_sha256,no_upscale:true,metadata_policy:'stripped'}))));
 if(link.profile_hash!==expected)throw new Error('COMPARE_PREVIEW_INVALID');
 let url:URL;try{url=new URL(link.url);}catch{throw new Error('COMPARE_PREVIEW_INVALID');}
 if(!['http:','https:'].includes(url.protocol)||!url.hostname||url.username||url.password||url.hash||url.protocol==='http:'&&!['localhost','[::1]'].includes(url.hostname)&&!/^127(?:\.[0-9]{1,3}){3}$/.test(url.hostname))throw new Error('COMPARE_PREVIEW_INVALID');
 return {versionId,sourceSha,iccSha:link.icc_sha256,previewURI:link.url};
}
