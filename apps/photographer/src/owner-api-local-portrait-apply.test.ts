import {expect,it,vi} from 'vitest';
import type {ApiClient} from '@photoeditor/api-client';
import type {Review} from './catalog-types';
import {OwnerAPI} from './owner-api';
import {readLocalPortraitAppliedVersion} from './local-portrait-applied-version';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const image={artifact_id:id(4),sha256:'a'.repeat(64)};
const review:Review={id:id(1),run_id:id(2),kind:'portrait.hair',state:'decided',revision:2,candidate_hash:image.sha256,candidate:image,base:{artifact_id:id(5),sha256:'b'.repeat(64)},decision:'accept',decision_hash:'c'.repeat(64),decision_route:'candidate',reasons:[],created_at:'2026-10-16T00:00:00Z'};
const selection={schema_version:1 as const,review_id:review.id,decision_hash:review.decision_hash!,route:'candidate' as const,image};
const result={asset_id:id(6),project_id:id(7),asset_version_id:id(8),review_id:review.id,decision_hash:selection.decision_hash,source:image};
it('sends the immutable human decision only and binds the returned version to its selected image',async()=>{
 const request=vi.fn().mockResolvedValueOnce(selection).mockResolvedValueOnce(result);
 const api=new OwnerAPI({request} as unknown as ApiClient,id(9)),signal=new AbortController().signal;
 expect(await api.applyLocalPortraitSelected(review,signal)).toEqual(result);
 expect(request.mock.calls[1][0]).toBe(`/api/v1/tenants/${id(9)}/reviews/${review.id}/local-portrait-apply`);
 expect(request.mock.calls[1][1]).toMatchObject({method:'POST',body:{decision_hash:selection.decision_hash},signal});
});
it('refuses pending and skipped reviews without applying',async()=>{
 const request=vi.fn(),api=new OwnerAPI({request} as unknown as ApiClient,id(9));
 await expect(api.applyLocalPortraitSelected({...review,state:'pending',revision:1})).rejects.toThrow();
 expect(request).not.toHaveBeenCalled();
 request.mockResolvedValueOnce({...selection,route:'skip',image:null});
 await expect(api.applyLocalPortraitSelected({...review,decision:'skip',decision_route:'skip'})).rejects.toThrow();
 expect(request).toHaveBeenCalledTimes(1);
});
it('rejects mismatched source, decision, version and extra response fields',()=>{
 for(const changed of [{...result,source:review.base},{...result,decision_hash:'d'.repeat(64)},{...result,review_id:id(20)},{...result,asset_version_id:'bad'},{...result,extra:true}]){
  expect(()=>readLocalPortraitAppliedVersion(changed,selection)).toThrow('LOCAL_PORTRAIT_APPLY_RESULT_INVALID');
 }
});

it('applies the rejected candidate as the exact original image selected by the photographer',async()=>{
 const original={...selection,route:'base' as const,image:review.base!};
 const originalVersion={...result,source:review.base!};
 const request=vi.fn().mockResolvedValueOnce(original).mockResolvedValueOnce(originalVersion);
 const api=new OwnerAPI({request} as unknown as ApiClient,id(9));
 expect(await api.applyLocalPortraitSelected({...review,decision:'reject',decision_route:'base'})).toEqual(originalVersion);
 expect(request.mock.calls[1][1]).toMatchObject({method:'POST',body:{decision_hash:selection.decision_hash}});
});
