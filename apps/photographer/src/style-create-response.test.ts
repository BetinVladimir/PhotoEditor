import {expect,it,vi} from 'vitest';
import type {ApiClient} from '@photoeditor/api-client';
import {OwnerAPI} from './owner-api';
const tenant='11111111-1111-4111-8111-111111111111',project='22222222-2222-4222-8222-222222222222',id='33333333-3333-4333-8333-333333333333';
const row={id,tenant_id:tenant,project_id:project,name:'Wedding',description:'Original',revision:'1',archived:false,created_at:'2026-10-17T10:00:00Z'};
it('validates created style scope and frozen request fields while preserving exact key',async()=>{
 const request=vi.fn().mockResolvedValue(row),api=new OwnerAPI({request} as unknown as ApiClient,tenant);
 expect((await api.createStyle(project,' Wedding ','Original','same-key')).id).toBe(id);
 expect(request.mock.calls[0][1].idempotencyKey).toBe('same-key');
 for(const changed of [{project_id:id},{tenant_id:project},{name:'Other'},{description:'Other'},{revision:'2'},{archived:true}]){
  request.mockResolvedValue({...row,...changed});await expect(api.createStyle(project,'Wedding','Original','same-key')).rejects.toThrow();
 }
});
it('rejects late style creation response after cancellation',async()=>{
 let settle!:(v:unknown)=>void;const request=vi.fn(()=>new Promise<unknown>(r=>{settle=r}));const api=new OwnerAPI({request} as unknown as ApiClient,tenant),c=new AbortController();
 const pending=api.createStyle(project,'Wedding','Original','same-key',c.signal),failure=expect(pending).rejects.toThrow();c.abort();settle(row);await failure;
});
