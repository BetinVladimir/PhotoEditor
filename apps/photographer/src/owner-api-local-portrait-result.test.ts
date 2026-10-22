import {expect,it,vi} from 'vitest';
import type {ApiClient} from '@photoeditor/api-client';
import {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const result={run_id:id(1),result_id:id(2),operation:'portrait.hair' as const,base:{artifact_id:id(3),sha256:'a'.repeat(64)},candidate:{artifact_id:id(4),sha256:'b'.repeat(64)},review_id:null};
const version={asset_id:id(5),project_id:id(6),asset_version_id:id(7),run_id:result.run_id,result_id:result.result_id,operation:result.operation,source:result.candidate};
it('reads the workflow result bound to the expected run and posts only its exact immutable selection',async()=>{
 const request=vi.fn().mockResolvedValueOnce(result).mockResolvedValueOnce(version),signal=new AbortController().signal;
 const api=new OwnerAPI({request} as unknown as ApiClient,id(9));
 const selected=await api.localPortraitResult(id(8),id(1),signal);
 expect(await api.applyLocalPortraitResult(id(8),selected,signal)).toEqual(version);
 expect(request.mock.calls[0][0]).toBe(`/api/v1/tenants/${id(9)}/v2-workflows/${id(8)}/local-portrait-result`);
 expect(request.mock.calls[1][0]).toBe(`/api/v1/tenants/${id(9)}/v2-workflows/${id(8)}/local-portrait-apply`);
 expect(request.mock.calls[1][1]).toMatchObject({method:'POST',body:{result_id:result.result_id,candidate_sha:result.candidate.sha256},signal});
});
it('refuses a result from a different run',async()=>{
 const request=vi.fn().mockResolvedValue(result),api=new OwnerAPI({request} as unknown as ApiClient,id(9));
 await expect(api.localPortraitResult(id(8),id(10))).rejects.toThrow('LOCAL_PORTRAIT_RESULT_INVALID');
 expect(request).toHaveBeenCalledTimes(1);
});
it('refuses reviewed or malformed selections before a mutation',async()=>{
 const request=vi.fn(),api=new OwnerAPI({request} as unknown as ApiClient,id(9));
 await expect(api.applyLocalPortraitResult(id(8),{...result,review_id:id(10)} as unknown as typeof result)).rejects.toThrow();
 expect(request).not.toHaveBeenCalled();
});
it('rejects a successful response bound to a different candidate',async()=>{
 const request=vi.fn().mockResolvedValue({...version,source:result.base}),api=new OwnerAPI({request} as unknown as ApiClient,id(9));
 await expect(api.applyLocalPortraitResult(id(8),result)).rejects.toThrow('LOCAL_PORTRAIT_AUTO_APPLY_INVALID');
});
