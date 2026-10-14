import {expect,it} from 'vitest';
import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import type {Review,RunOutput} from './catalog-types';
import {bindReviewPreview} from './review-preview-manifest';
const id=(n:number)=>`00000000-0000-4000-8000-${n.toString().padStart(12,'0')}`;
function fixture(){
 const source={artifact_id:id(3),sha256:'a'.repeat(64)},base={artifact_id:id(4),sha256:'b'.repeat(64)},profile='c'.repeat(64);
 const review:Review={id:id(1),run_id:id(2),kind:'optical.negative',state:'pending',revision:1,candidate_hash:source.sha256,candidate:source,base,reasons:[],created_at:'2026-10-10T00:00:00Z'};
 const image:RunOutput={artifact_id:id(5),sha256:'d'.repeat(64),role:'derivative',bytes:100,media_type:'image/jpeg',url:'https://media.test/image?versionId=v1',expires_at:'2030-01-01T00:00:00Z'};
 const manifest={source,derivative:{artifact_id:image.artifact_id,sha256:image.sha256},variant:'preview1600',profile_hash:profile,width:1600,height:1000,media_type:'image/jpeg',color_profile:'srgb',metadata_policy:'stripped',alpha:false};
 const body=new TextEncoder().encode(canonicalJSON(manifest));
 const document:RunOutput={artifact_id:id(6),role:'derivative_manifest',sha256:bytesToHex(sha256(body)),bytes:body.length,media_type:'application/json',url:'https://media.test/manifest?versionId=v2',expires_at:image.expires_at};
 return {review,image,manifest,body,document,profile};
}
it('binds one exact review side and owns its returned pins',()=>{const f=fixture(),result=bindReviewPreview(f.body,f.document,f.image,f.review,'after',f.profile,0);f.image.url='https://other.test';f.review.candidate!.sha256='e'.repeat(64);expect(result.image.url).toContain('media.test');expect(result.source.sha256).toBe('a'.repeat(64));expect(()=>bindReviewPreview(f.body,f.document,f.image,f.review,'before',f.profile,0)).toThrow()});
it.each(['hash','profile','source','alpha','dimensions','expiry','image_pin','unknown'])('rejects substituted preview: %s',mode=>{
 const f=fixture();if(mode==='profile')f.manifest.profile_hash='e'.repeat(64);if(mode==='source')f.manifest.source={...f.review.base!};if(mode==='alpha')f.manifest.alpha=true;if(mode==='dimensions')f.manifest.width=1601;if(mode==='unknown')(f.manifest as Record<string,unknown>).extra=true;
 const body=new TextEncoder().encode(canonicalJSON(f.manifest));f.document.bytes=body.length;f.document.sha256=bytesToHex(sha256(body));
 if(mode==='hash')f.document.sha256='e'.repeat(64);if(mode==='expiry')f.image.expires_at='1970-01-01T00:00:00Z';if(mode==='image_pin')f.image.sha256='e'.repeat(64);
 expect(()=>bindReviewPreview(body,f.document,f.image,f.review,'after',f.profile,0)).toThrow('REVIEW_PREVIEW_INVALID');
});
