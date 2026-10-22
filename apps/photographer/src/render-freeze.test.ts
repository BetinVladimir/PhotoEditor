import {it,expect,vi} from 'vitest';
import {OwnerAPI} from './owner-api';
import {renderFreezeRequest,renderFreezeResult,type RenderFreezeRequest} from './render-freeze';
const id='11111111-1111-1111-1111-111111111111',other='22222222-2222-2222-2222-222222222222';
const request=():RenderFreezeRequest=>({document_id:id,expected_revision:'9223372036854775807',assume_profile:null});
const result=()=>({document_id:other,draft_document_id:id,draft_revision:request().expected_revision,source:{asset_version_id:other,source:{artifact_id:id,sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)},recipe:{artifact_id:other,sha256:'c'.repeat(64)}});
it('snapshots intent and retains server pins, retry key and cancellation',async()=>{
 let resolve!:(value:unknown)=>void;const transport=vi.fn((..._args:unknown[])=>new Promise(r=>{resolve=r}));
 const api=new OwnerAPI({request:transport} as never,'33333333-3333-4333-8333-333333333333'),intent=request(),controller=new AbortController();
 const pending=api.freezeRenderDraft('44444444-4444-4444-8444-444444444444',intent,'retry-key',controller.signal);intent.expected_revision='1';
 expect(transport.mock.calls[0]).toEqual(['/api/v1/tenants/33333333-3333-4333-8333-333333333333/assets/44444444-4444-4444-8444-444444444444/v2-draft/freeze',expect.objectContaining({body:request(),idempotencyKey:'retry-key',signal:controller.signal})]);
 const wire=result();resolve(wire);const accepted=await pending;wire.recipe.sha256='f'.repeat(64);expect(accepted.recipe.sha256).toBe('c'.repeat(64));
});
it('rejects invalid intent and mismatched server replies',()=>{
 for(const revision of ['0','01','+1','9223372036854775808'])expect(()=>renderFreezeRequest({...request(),expected_revision:revision})).toThrow();
 expect(()=>renderFreezeRequest({...request(),runtime:'cuda'} as RenderFreezeRequest)).toThrow();
 expect(()=>renderFreezeResult({...result(),draft_revision:'1'},request())).toThrow();
 expect(()=>renderFreezeResult({...result(),document_id:id},request())).toThrow();
});

it('refuses malformed scope/preabort before freeze and late replies after cancellation',async()=>{
 const transport=vi.fn();const api=new OwnerAPI({request:transport} as never,'33333333-3333-4333-8333-333333333333'),c=new AbortController();
 await expect(api.freezeRenderDraft('bad',request(),'same-key',c.signal)).rejects.toThrow();c.abort();await expect(api.freezeRenderDraft(other,request(),'same-key',c.signal)).rejects.toThrow();expect(transport).not.toHaveBeenCalled();
 let resolve!:(v:unknown)=>void;const pendingAPI=new OwnerAPI({request:()=>new Promise(r=>{resolve=r})} as never,'33333333-3333-4333-8333-333333333333'),late=new AbortController();
 const pending=pendingAPI.freezeRenderDraft(other,request(),'same-key',late.signal),rejected=expect(pending).rejects.toThrow();late.abort();resolve(result());await rejected;
});
