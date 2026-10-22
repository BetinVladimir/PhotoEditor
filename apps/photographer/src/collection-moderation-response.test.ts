import {expect,it,vi} from 'vitest';
import type {ApiClient} from '@photoeditor/api-client';
import {OwnerAPI} from './owner-api';
import type {Collection,CollectionItem} from './catalog-types';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const collection=()=>({id:id(1),tenant_id:id(2),revision:'7'} as Collection);
const item=()=>({id:id(3),collection_id:id(1),asset_id:id(4),asset_version_id:id(5),position:0,visibility:'hidden',moderation_state:'pending_review',rating:0,flag:'none',tags:[],created_at:'2026-10-17T12:00:00Z'} as CollectionItem);
it('validates the exact complete moderation receipt and captured item identity after IO',async()=>{
 let resolve!:(value:unknown)=>void;const request=vi.fn().mockImplementation(()=>new Promise<unknown>(done=>{resolve=done}));const api=new OwnerAPI({request} as unknown as ApiClient,id(2)),before=item();
 const pending=api.moderateApplyItem(collection(),before,'accept');before.asset_version_id=id(9);
 resolve({...item(),moderation_state:'accepted'});expect((await pending).asset_version_id).toBe(id(5));
 for(const patch of [{id:id(9)},{asset_version_id:id(9)},{moderation_state:'rejected'},{created_at:null},{bucket:'private'}]){
  request.mockResolvedValueOnce({...item(),moderation_state:'accepted',...patch});await expect(api.moderateApplyItem(collection(),item(),'accept')).rejects.toThrow();
 }
});
it('refuses foreign tenant and preaborted calls before transport, late abort before acknowledgement',async()=>{
 const controller=new AbortController(),request=vi.fn(),api=new OwnerAPI({request} as unknown as ApiClient,id(2));controller.abort();
 await expect(api.moderateApplyItem(collection(),item(),'accept',controller.signal)).rejects.toThrow();
 await expect(api.moderateApplyItem({...collection(),tenant_id:id(9)},item(),'accept')).rejects.toThrow();expect(request).not.toHaveBeenCalled();
 const late=new AbortController();request.mockImplementation(async()=>{late.abort();return {...item(),moderation_state:'accepted'}});
 await expect(api.moderateApplyItem(collection(),item(),'accept',late.signal)).rejects.toThrow();
});
