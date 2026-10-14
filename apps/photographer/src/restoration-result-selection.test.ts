import {expect,it,vi} from 'vitest';
import type {Review} from './catalog-types';
import {readRestorationResultSelection} from './negative-result-selection';
import {OwnerAPI} from './owner-api';

const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const review=(kind:'portrait.glare'|'portrait.open_eyes'|'image.reflection',decision:'accept'|'reject'|'skip'):Review=>({id:id(1),run_id:id(2),kind,state:'decided',revision:2,candidate_hash:'a'.repeat(64),candidate:{artifact_id:id(3),sha256:'a'.repeat(64)},base:{artifact_id:id(4),sha256:'b'.repeat(64)},decision,decision_route:decision==='accept'?'candidate':decision==='reject'?'base':'skip',reasons:[],created_at:'2026-10-13T00:00:00Z'});

it.each(['portrait.glare','portrait.open_eyes','image.reflection'] as const)('binds %s selected image to the human route',kind=>{
 for(const decision of ['accept','reject','skip'] as const){
  const current=review(kind,decision),image=decision==='accept'?current.candidate:decision==='reject'?current.base:null;
  const raw={schema_version:1,review_id:current.id,decision_hash:'c'.repeat(64),route:current.decision_route,image};
  expect(readRestorationResultSelection(raw,current).image).toEqual(image);
  expect(()=>readRestorationResultSelection({...raw,image:decision==='skip'?current.candidate:decision==='accept'?current.base:current.candidate},current)).toThrow('RESTORATION_RESULT_SELECTION_INVALID');
 }
});

it('uses only the decided restoration selection and preview routes',async()=>{
 const current=review('portrait.glare','reject'),signal=new AbortController().signal;
 const raw={schema_version:1,review_id:current.id,decision_hash:'c'.repeat(64),route:'base',image:current.base};
 const request=vi.fn().mockResolvedValueOnce(raw).mockResolvedValueOnce({review_id:current.id});
 const api=new OwnerAPI({request} as never,id(5));
 expect((await api.restorationResultSelection(current)).image).toEqual(current.base);
 await api.restorationSelectedPreview(current,signal);
 expect(request.mock.calls.map(call=>call[0])).toEqual([
  `/api/v1/tenants/${id(5)}/reviews/${current.id}/restoration-result-selection`,
  `/api/v1/tenants/${id(5)}/reviews/${current.id}/restoration-selected-preview/preview1600`,
 ]);
 current.state='pending';current.revision=1;
 await expect(api.restorationResultSelection(current)).rejects.toThrow();
 expect(()=>api.restorationSelectedPreview(current,signal)).toThrow();
 expect(request).toHaveBeenCalledTimes(2);
});
