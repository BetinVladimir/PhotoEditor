import {describe,it,expect,vi} from 'vitest';
import {decodeSemanticTargets,listSemanticTargets} from './semantic';
const id='11111111-1111-4111-8111-111111111111',other='22222222-2222-4222-8222-222222222222';
function fixture(){return {items:[{id,revision:'9007199254740993',document:null,data:{resource_type:'semantic_targets',id,tenant_id:id,project_id:id,created_at:'2026-10-04T12:00:00Z',revision:'9007199254740993',updated_at:'2026-10-04T12:00:00Z',intent_id:id,child_run_id:other,batch_id:other,asset_version_id:id,candidate_artifact_id:null,review_id:null,override_id:null,state:'applied'}}],next_cursor:null}}
describe('semantic targets',()=>{
 it('preserves decimal revision and owns the response',()=>{const raw=fixture();const result=decodeSemanticTargets(raw,id,id);raw.items[0].data.state='failed';expect(result.items[0].data.state).toBe('applied');expect(result.items[0].revision).toBe('9007199254740993')});
 it.each(['scope','revision','state','duplicate','extra'])('rejects %s substitution',(kind)=>{const raw=fixture();if(kind==='scope')raw.items[0].data.tenant_id=other;if(kind==='revision')raw.items[0].revision='2';if(kind==='state')raw.items[0].data.state='approved';if(kind==='duplicate')raw.items.push(raw.items[0]);if(kind==='extra')Object.assign(raw,{object_key:'private'});expect(()=>decodeSemanticTargets(raw,id,id)).toThrow('SEMANTIC_TARGET_RESPONSE_INVALID')});
 it('rejects invalid query before IO',async()=>{const request=vi.fn();await expect(listSemanticTargets({request},id,id,{limit:101})).rejects.toThrow('QUERY_INVALID');expect(request).not.toHaveBeenCalled()});
 it('escapes cursor and validates the fetched response',async()=>{const request=vi.fn().mockResolvedValue(fixture());await listSemanticTargets({request},id,id,{limit:1,cursor:'a+b&c'});expect(request.mock.calls[0][0]).toContain('limit=1&cursor=a%2Bb%26c')});
});

it.each(['1','9007199254740993','9223372036854775807'])('accepts exact int64 target revision %s',(revision)=>{
 const raw=fixture();raw.items[0].revision=revision;raw.items[0].data.revision=revision;
 expect(decodeSemanticTargets(raw,id,id).items[0].revision).toBe(revision);
});
it.each(['0','01','-1','1.0','9223372036854775808','9999999999999999999','10000000000000000000'])('rejects out-of-domain target revision %s',(revision)=>{
 const raw=fixture();raw.items[0].revision=revision;raw.items[0].data.revision=revision;
 expect(()=>decodeSemanticTargets(raw,id,id)).toThrow('SEMANTIC_TARGET_RESPONSE_INVALID');
});

it.each([1,[],{},null])('refuses non-string cursor before transport: %s',async cursor=>{
 const request=vi.fn();await expect(listSemanticTargets({request},id,id,{cursor} as never)).rejects.toThrow('SEMANTIC_TARGET_QUERY_INVALID');expect(request).not.toHaveBeenCalled();
});
it.each(['2026-10-04T12:00:00','2026-10-04T12:00Z'])('refuses timezone-less or reduced Go row timestamps %s',created_at=>{
 const value=fixture();value.items[0].data.created_at=created_at;expect(()=>decodeSemanticTargets(value,id,id)).toThrow();
});
