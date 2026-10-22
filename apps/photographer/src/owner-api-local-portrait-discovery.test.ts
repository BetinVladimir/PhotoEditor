import {expect,it,vi} from 'vitest';
import type {ApiClient} from '@photoeditor/api-client';
import {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
it('binds discovery to the requested run and propagates cancellation',async()=>{
 const signal=new AbortController().signal,request=vi.fn().mockResolvedValue({run_id:id(1),workflow_id:id(2)}),api=new OwnerAPI({request} as unknown as ApiClient,id(3));
 expect(await api.discoverLocalPortraitResult(id(1),signal)).toEqual({run_id:id(1),workflow_id:id(2)});expect(request.mock.calls[0][0]).toBe(`/api/v1/tenants/${id(3)}/runs/${id(1)}/local-portrait-result`);expect(request.mock.calls[0][1]).toMatchObject({signal});
});
it.each([{run_id:id(4),workflow_id:id(2)},{run_id:id(1),workflow_id:'invalid'},{run_id:id(1),workflow_id:null,extra:true}])('rejects foreign or malformed discovery',async(value)=>{
 const api=new OwnerAPI({request:vi.fn().mockResolvedValue(value)} as unknown as ApiClient,id(3));await expect(api.discoverLocalPortraitResult(id(1))).rejects.toThrow('LOCAL_PORTRAIT_DISCOVERY_INVALID');
});
