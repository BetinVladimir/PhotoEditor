import {expect,it,vi} from 'vitest';
import type {Review} from './catalog-types';
import {readRenderResultSelection} from './negative-result-selection';
import {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const review=(decision:'accept'|'reject'|'skip'):Review=>({id:id(1),run_id:id(2),kind:'render.compose',state:'decided',revision:2,candidate_hash:'a'.repeat(64),candidate:{artifact_id:id(3),sha256:'a'.repeat(64)},base:{artifact_id:id(4),sha256:'b'.repeat(64)},decision,decision_route:decision==='accept'?'candidate':decision==='reject'?'base':'skip',reasons:[],created_at:'2026-10-13T00:00:00Z'});
it.each(['accept','reject','skip'] as const)('binds render %s to the selected image',decision=>{
 const current=review(decision),image=decision==='accept'?current.candidate:decision==='reject'?current.base:null;
 const raw={schema_version:1,review_id:current.id,decision_hash:'c'.repeat(64),route:current.decision_route,image};
 const parsed=readRenderResultSelection(raw,current);
 expect(parsed.image).toEqual(image);if(image)expect(parsed.image).not.toBe(image);
 const changed={...raw,image:decision==='skip'?current.candidate:decision==='accept'?current.base:current.candidate};
 expect(()=>readRenderResultSelection(changed,current)).toThrow('RENDER_RESULT_SELECTION_INVALID');
});
it('requests only the render route after validating the decided binding',async()=>{
 const current=review('reject'),raw={schema_version:1,review_id:current.id,decision_hash:'c'.repeat(64),route:'base',image:current.base};
 const request=vi.fn().mockResolvedValue(raw),api=new OwnerAPI({request} as never,id(5));
 expect((await api.renderResultSelection(current)).route).toBe('base');
 expect(request.mock.calls[0][0]).toBe(`/api/v1/tenants/${id(5)}/reviews/${id(1)}/render-result-selection`);
 const changed=review('reject');changed.state='pending';changed.revision=1;
 await expect(api.renderResultSelection(changed)).rejects.toThrow();expect(request).toHaveBeenCalledTimes(1);
});
it.each(['missing-base','candidate-hash','aliased-base'])('refuses a skipped render result with %s',mode=>{
 const current=review('skip');
 if(mode==='missing-base')delete current.base;
 if(mode==='candidate-hash')current.candidate_hash='d'.repeat(64);
 if(mode==='aliased-base')current.base!.artifact_id=current.candidate!.artifact_id;
 expect(()=>readRenderResultSelection({schema_version:1,review_id:current.id,decision_hash:'c'.repeat(64),route:'skip',image:null},current)).toThrow('RENDER_RESULT_SELECTION_INVALID');
});
it('binds the receipt to the exact decided review hash when present',()=>{
 const current=review('accept');current.decision_hash='d'.repeat(64);
 const receipt={schema_version:1,review_id:current.id,decision_hash:'c'.repeat(64),route:'candidate',image:current.candidate};
 expect(()=>readRenderResultSelection(receipt,current)).toThrow('RENDER_RESULT_SELECTION_INVALID');
 expect(readRenderResultSelection({...receipt,decision_hash:current.decision_hash},current).decision_hash).toBe(current.decision_hash);
});
it('uses the selected render preview route only after a decided binding',async()=>{
 const current=review('reject'),request=vi.fn().mockResolvedValue({review_id:current.id});
 const api=new OwnerAPI({request} as never,id(5));
 const signal=new AbortController().signal;
 await api.renderSelectedPreview(current,signal);
 expect(request.mock.calls[0][0]).toBe(`/api/v1/tenants/${id(5)}/reviews/${current.id}/selected-preview/preview1600`);
 current.state='pending';current.revision=1;
 await expect(api.renderSelectedPreview(current,signal)).rejects.toThrow();
 expect(request).toHaveBeenCalledTimes(1);
});
