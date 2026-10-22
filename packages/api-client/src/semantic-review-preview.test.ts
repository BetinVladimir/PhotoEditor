import {expect,it,vi} from 'vitest';
import {decodeSemanticPreviewAdmission,decodeSemanticReviewPreview,readSemanticReviewPreview,semanticPreviewQuery,semanticPreviewStillCurrent,type SemanticPreviewQuery,type SemanticReviewPreview} from './semantic-review-preview';
const id=(n:number)=>`${n.toString(16).padStart(8,'0')}-0000-4000-8000-000000000001`;
const now=Date.parse('2026-10-15T10:00:00Z');
const query:SemanticPreviewQuery={tenant_id:id(1),intent_id:id(2),target_id:id(3),target_revision:'9007199254740993',asset_version_id:id(4),workflow_id:id(5),workflow_revision:'9',plan_sha:'a'.repeat(64),candidate:{artifact_id:id(6),sha256:'b'.repeat(64)}};
function fixture():SemanticReviewPreview{
 const media=(n:number)=>({artifact_id:id(n),sha256:'c'.repeat(64),url:`https://media.example/${n}`,expires_at:new Date(now+60000).toISOString(),width:1024,height:768,media_type:'image/png' as const});
 return {...structuredClone(query),schema_version:1,render:{artifact_id:id(7),sha256:'d'.repeat(64)},evidence:{artifact_id:id(8),sha256:'e'.repeat(64)},quality_status:'review',limitations:['Review the edge of the affected region.'],protected_pixels_unchanged:true,before:media(9),after:media(10),affected:media(11),protected:media(12)};
}
it('owns exact fenced preview bindings and preserves large decimal revisions',()=>{
 const raw=fixture(),owned=decodeSemanticReviewPreview(raw,query,now);raw.candidate.sha256='f'.repeat(64);raw.limitations.push('changed');
 expect(owned.target_revision).toBe(query.target_revision);expect(owned.candidate.sha256).toBe(query.candidate.sha256);expect(owned.limitations).toHaveLength(1);
 expect(semanticPreviewStillCurrent(owned,query,now)).toBe(true);expect(semanticPreviewStillCurrent(owned,query,now+60000)).toBe(false);
});
it.each(['tenant','target','version','plan','revision','candidate','missing_protection','dimensions','alias','expiry','long_expiry','unsafe_url','extra','quality'])('refuses %s substitutions',kind=>{
 const value=fixture();
 if(kind==='tenant')value.tenant_id=id(99);if(kind==='target')value.target_id=id(99);if(kind==='version')value.asset_version_id=id(99);
 if(kind==='plan')value.plan_sha='f'.repeat(64);if(kind==='revision')value.workflow_revision='10';if(kind==='candidate')value.candidate.sha256='f'.repeat(64);
 if(kind==='missing_protection')Object.assign(value,{protected_pixels_unchanged:false});if(kind==='dimensions')value.after.width++;
 if(kind==='alias')value.after.artifact_id=value.before.artifact_id;if(kind==='expiry')value.before.expires_at=new Date(now).toISOString();if(kind==='long_expiry')value.after.expires_at=new Date(now+300001).toISOString();
 if(kind==='unsafe_url')value.before.url='file:///private/image';if(kind==='extra')Object.assign(value,{object_key:'private'});if(kind==='quality')Object.assign(value,{quality_status:'unsupported'});
 expect(()=>decodeSemanticReviewPreview(value,query,now)).toThrow('SEMANTIC_REVIEW_PREVIEW_INVALID');
});
it('rejects a proposed target without a pinned candidate before network IO',()=>{
 expect(()=>semanticPreviewQuery(query.tenant_id,query.intent_id,{id:query.target_id,revision:'1',document:null,data:{state:'proposed'}} as never,{} as never)).toThrow('SEMANTIC_PREVIEW_BINDING_INVALID');
});
it('reads only the current fenced target with abort propagation',async()=>{
 vi.spyOn(Date,'now').mockReturnValue(now);
 try{const request=vi.fn().mockResolvedValue(fixture()),signal=new AbortController().signal;
 await readSemanticReviewPreview({request},query,signal);
 expect(request).toHaveBeenCalledWith(`/api/v1/tenants/${query.tenant_id}/semantic-intents/${query.intent_id}/targets/${query.target_id}/preview?expected_revision=9&target_revision=${query.target_revision}&plan_sha=${query.plan_sha}&candidate_sha=${query.candidate.sha256}`,{signal});
 }finally{vi.restoreAllMocks()}
});

it('rejects coerced quality enums and document/media aliases',()=>{
 for(const quality_status of [['pass'],['review'],{toString:()=> 'pass'}])expect(()=>decodeSemanticReviewPreview({...fixture(),quality_status},query,now)).toThrow();
 for(const ref of [query.candidate,fixture().render,fixture().evidence]){
  const value=fixture();value.after.artifact_id=ref.artifact_id;
  expect(()=>decodeSemanticReviewPreview(value,query,now)).toThrow();
 }
 const value=fixture();value.limitations=['😀'.repeat(500)];
 expect(decodeSemanticReviewPreview(value,query,now).limitations).toEqual(value.limitations);
});
it('does not return a preview after request cancellation even if transport resolves',async()=>{
 const controller=new AbortController(),request=vi.fn().mockImplementation(async()=>{controller.abort();return fixture()});
 await expect(readSemanticReviewPreview({request} as never,query,controller.signal)).rejects.toThrow();
});

it('rejects aliased admission identities',async()=>{
 const {decodeSemanticPreviewAdmission}=await import('./semantic-review-preview');
 expect(()=>decodeSemanticPreviewAdmission({run_id:'11111111-1111-4111-8111-111111111111',task_id:'11111111-1111-4111-8111-111111111111',status:'queued',commit_attempted:true,recovered:false})).toThrow();
});

it('posts an empty body with immutable preview query fences',async()=>{
 const {admitSemanticReviewPreview}=await import('./semantic-review-preview');
 const request=vi.fn(async()=>({run_id:id(20),task_id:id(21),status:'queued',commit_attempted:true,recovered:false}));
 await admitSemanticReviewPreview({request} as never,query);
 expect(request).toHaveBeenCalledWith(expect.stringContaining('/preview?expected_revision=9&'),{method:'POST',signal:undefined});
});


it.each([['queued'], ['failed'], {toString:()=> 'running'}, null, 1, true].map(status=>[status]))('rejects coerced admission status %s',async(status)=>{
 const {decodeSemanticPreviewAdmission}=await import('./semantic-review-preview');
 expect(()=>decodeSemanticPreviewAdmission({run_id:id(20),task_id:id(21),status,commit_attempted:true,recovered:false})).toThrow('SEMANTIC_PREVIEW_ADMISSION_INVALID');
});

it('keeps exact admission query during asynchronous caller mutation',async()=>{
 const {admitSemanticReviewPreview}=await import('./semantic-review-preview');
 const input=structuredClone(query);
 const request=vi.fn(async()=>{
  input.tenant_id=id(99);input.workflow_revision='10';input.target_revision='11';input.candidate.sha256='f'.repeat(64);
  return {run_id:id(20),task_id:id(21),status:'queued',commit_attempted:true,recovered:false};
 });
 await admitSemanticReviewPreview({request} as never,input);
 expect(request).toHaveBeenCalledWith(`/api/v1/tenants/${query.tenant_id}/semantic-intents/${query.intent_id}/targets/${query.target_id}/preview?expected_revision=9&target_revision=${query.target_revision}&plan_sha=${query.plan_sha}&candidate_sha=${query.candidate.sha256}`,{method:'POST',signal:undefined});
});

it('does not return late admission after transport cancels the request',async()=>{
 const {admitSemanticReviewPreview}=await import('./semantic-review-preview');
 const controller=new AbortController();
 const request=vi.fn(async()=>{
  controller.abort();
  return {run_id:id(20),task_id:id(21),status:'queued',commit_attempted:true,recovered:false};
 });
 await expect(admitSemanticReviewPreview({request} as never,query,controller.signal)).rejects.toThrow();
});


it.each(['before','after','affected','protected'] as const)('rejects coerced %s media types before accepting a preview',role=>{
 for(const invalid of [['image/png'],{toString:()=> 'image/png'},null,1]){
  const value=fixture();Object.assign(value[role],{media_type:invalid});
  expect(()=>decodeSemanticReviewPreview(value,query,now)).toThrow('SEMANTIC_REVIEW_PREVIEW_INVALID');
 }
});


it('preserves a recovered cancelling preview identity without inventing a new commit',()=>{
 const value={run_id:id(20),task_id:id(21),status:'cancelling',commit_attempted:false,recovered:true};
 expect(decodeSemanticPreviewAdmission(value)).toEqual(value);
});
