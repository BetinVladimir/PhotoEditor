import {it,expect,vi} from 'vitest';
import {OwnerAPI} from './owner-api';
import {renderFreezeRequest,renderFreezeResult,type RenderFreezeRequest} from './render-freeze';
const id='11111111-1111-1111-1111-111111111111',other='22222222-2222-2222-2222-222222222222';
const request=():RenderFreezeRequest=>({document_id:id,expected_revision:'9223372036854775807',assume_profile:null});
const result=()=>({document_id:other,draft_document_id:id,draft_revision:request().expected_revision,source:{asset_version_id:other,source:{artifact_id:id,sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)},recipe:{artifact_id:other,sha256:'c'.repeat(64)}});
it('snapshots intent and retains server pins, retry key and cancellation',async()=>{
 let resolve!:(value:unknown)=>void;const transport=vi.fn((..._args:unknown[])=>new Promise(r=>{resolve=r}));
 const api=new OwnerAPI({request:transport} as never,'tenant/id'),intent=request(),controller=new AbortController();
 const pending=api.freezeRenderDraft('asset/id',intent,'retry-key',controller.signal);intent.expected_revision='1';
 expect(transport.mock.calls[0]).toEqual(['/api/v1/tenants/tenant%2Fid/assets/asset%2Fid/v2-draft/freeze',expect.objectContaining({body:request(),idempotencyKey:'retry-key',signal:controller.signal})]);
 const wire=result();resolve(wire);const accepted=await pending;wire.recipe.sha256='f'.repeat(64);expect(accepted.recipe.sha256).toBe('c'.repeat(64));
});
it('rejects invalid intent and mismatched server replies',()=>{
 for(const revision of ['0','01','+1','9223372036854775808'])expect(()=>renderFreezeRequest({...request(),expected_revision:revision})).toThrow();
 expect(()=>renderFreezeRequest({...request(),runtime:'cuda'} as RenderFreezeRequest)).toThrow();
 expect(()=>renderFreezeResult({...result(),draft_revision:'1'},request())).toThrow();
 expect(()=>renderFreezeResult({...result(),document_id:id},request())).toThrow();
});
