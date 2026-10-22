import {expect,it,vi} from 'vitest';
import type {ApiClient} from '@photoeditor/api-client';
import {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const asset={id:id(1),tenant_id:id(2),project_id:id(3)};
it('resolves the actual asset project through the scoped authenticated read',async()=>{
 const request=vi.fn().mockResolvedValue(asset),api=new OwnerAPI({request} as unknown as ApiClient,id(2));
 expect(await api.assetProject(id(1))).toBe(id(3));
 expect(request.mock.calls[0][0]).toBe(`/api/v1/tenants/${id(2)}/assets/${id(1)}`);
 expect(request.mock.calls[0][1].signal).toBeInstanceOf(AbortSignal);
});
it.each([{...asset,id:id(4)},{...asset,tenant_id:id(4)},{...asset,project_id:'../foreign'},{...asset,project_id:'00000000-0000-0000-0000-000000000000'},{...asset,project_id:null}])('refuses forged or invalid project authority %j',async value=>{
 const request=vi.fn().mockResolvedValue(value),api=new OwnerAPI({request} as unknown as ApiClient,id(2));
 await expect(api.assetProject(id(1))).rejects.toThrow();
});
it('refuses malformed asset identity before transport',async()=>{
 const request=vi.fn(),api=new OwnerAPI({request} as unknown as ApiClient,id(2));
 await expect(api.assetProject('../foreign')).rejects.toThrow();expect(request).not.toHaveBeenCalled();
});
it('cannot publish an asset project after caller cancellation even when transport resolves late',async()=>{
 let resolve!:(value:typeof asset)=>void;
 const request=vi.fn().mockImplementation(()=>new Promise(done=>{resolve=done}));
 const api=new OwnerAPI({request} as unknown as ApiClient,id(2)),controller=new AbortController();
 const pending=api.assetProject(id(1),controller.signal);
 const assertion=expect(pending).rejects.toThrow();
 controller.abort();resolve(asset);await assertion;
});
