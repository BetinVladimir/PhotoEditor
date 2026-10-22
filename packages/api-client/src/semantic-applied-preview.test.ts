import {it,expect,vi} from 'vitest';
import {decodeSemanticAppliedPreview,readSemanticAppliedPreview} from './semantic-applied-preview';
const tenant='11111111-1111-4111-8111-111111111111',target='22222222-2222-4222-8222-222222222222';
const now=Date.parse('2026-10-17T12:00:00Z');
const fixture=()=>({schema_version:1,tenant_id:tenant,target_id:target,child_run_id:tenant,working_image:{artifact_id:tenant,sha256:'a'.repeat(64)},after:{artifact_id:target,sha256:'b'.repeat(64),width:100,height:90,media_type:'image/png',url:'https://objects.example/exact-target',expires_at:new Date(now+60000).toISOString()}});
it('decodes exact target and immutable image reference',()=>expect(decodeSemanticAppliedPreview(fixture(),tenant,target,now).target_id).toBe(target));
it('rejects foreign target, run-level DTO, expired URL and unknown fields',()=>{
 for(const value of [{...fixture(),target_id:tenant},{items:[]},{...fixture(),extra:true},{...fixture(),after:{...fixture().after,expires_at:new Date(now).toISOString()}},{...fixture(),after:{...fixture().after,url:'https://user:secret@objects.example/x'}}])expect(()=>decodeSemanticAppliedPreview(value,tenant,target,now)).toThrow();
});
it('uses target endpoint and forwards cancellation',async()=>{
 const signal=new AbortController().signal,request=vi.fn().mockResolvedValue({...fixture(),after:{...fixture().after,expires_at:new Date(Date.now()+60000).toISOString()}});
 await readSemanticAppliedPreview({request},tenant,target,signal);
 expect(request).toHaveBeenCalledWith(`/api/v1/tenants/${tenant}/semantic-targets/${target}/preview`,{signal});
});
