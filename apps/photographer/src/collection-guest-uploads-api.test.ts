import {expect,it,vi} from 'vitest';
import {CollectionGuestUploadsAPI,decodeGuestUpload} from './collection-guest-uploads-api';
const tenant='11111111-1111-4111-8111-111111111111',collection='22222222-2222-4222-8222-222222222222',upload='33333333-3333-4333-8333-333333333333',item='44444444-4444-4444-8444-444444444444',version='55555555-5555-4555-8555-555555555555';
const row={id:upload,collection_id:collection,share_id:tenant,filename:'portrait.jpg',state:'verified',revision:'17',expected_size:200,actual_size:200,created_at:'2026-10-17T00:00:00Z',item_id:item,asset_version_id:version,moderation_state:'pending_review' as const};
it('refuses foreign rows, partial membership and internal storage fields',()=>{
 for(const bad of [{...row,collection_id:tenant},{...row,item_id:undefined},{...row,object_key:'private/path'},{...row,actual_size:201}])expect(()=>decodeGuestUpload(bad,collection)).toThrow('GUEST_UPLOAD_INVALID');
 expect(decodeGuestUpload({...row,revision:'9223372036854775807'},collection).revision).toBe('9223372036854775807');
});
it('accepts actual Go offsets/nanoseconds and refuses reduced date strings',()=>{
 for(const created_at of ['1','2026-10-17','2026-10-17T00:00:00','2026-10-17T00:00:00.1234567890Z'])expect(()=>decodeGuestUpload({...row,created_at},collection)).toThrow('GUEST_UPLOAD_INVALID');
 for(const created_at of ['2026-10-17T00:00:00.123456789Z','2026-10-17T03:00:00.123456+03:00'])expect(decodeGuestUpload({...row,created_at},collection).created_at).toBe(created_at);
});
it('rejects duplicate results and repeated cursors',async()=>{
 const request=vi.fn().mockResolvedValueOnce({items:[row,row],next_cursor:null}).mockResolvedValueOnce({items:[row],next_cursor:'same'});
 const api=new CollectionGuestUploadsAPI({request},tenant,collection),signal=new AbortController().signal;
 await expect(api.list(signal)).rejects.toThrow('GUEST_UPLOAD_PAGE_INVALID');
 await expect(api.list(signal,'same')).rejects.toThrow('GUEST_UPLOAD_PAGE_INVALID');
});
it('freezes exact upload revision and item/version before awaiting a moderation receipt',async()=>{
 let finish!:(v:unknown)=>void;const request=vi.fn((_path:string,_options?:unknown)=>new Promise(r=>{finish=r;}));
 const api=new CollectionGuestUploadsAPI({request} as never,tenant,collection),source={...row};
 const pending=api.moderate(source,'accept',new AbortController().signal);source.item_id=tenant;source.asset_version_id=tenant;source.revision='99';
 finish({id:item,collection_id:collection,asset_id:tenant,asset_version_id:version,visibility:'hidden',moderation_state:'accepted',position:0});await pending;
 expect(request.mock.calls[0][0]).toContain(`/uploads/${upload}/moderation`);
 expect(request.mock.calls[0][1]).toMatchObject({method:'POST',body:{decision:'accept',expected_revision:17}});
});
it('refuses substituted accepted output and unsafe numeric CAS before transport',async()=>{
 const request=vi.fn().mockResolvedValue({id:tenant,collection_id:collection,asset_id:tenant,asset_version_id:version,visibility:'hidden',moderation_state:'accepted',position:0});
 const api=new CollectionGuestUploadsAPI({request},tenant,collection),signal=new AbortController().signal;
 await expect(api.moderate(row,'accept',signal)).rejects.toThrow('GUEST_UPLOAD_RECEIPT_INVALID');request.mockClear();
 await expect(api.moderate({...row,revision:'9007199254740992'},'accept',signal)).rejects.toThrow('GUEST_UPLOAD_NOT_REVIEWABLE');expect(request).not.toHaveBeenCalled();
});
it('preaborted reads and decisions issue no request',async()=>{
 const request=vi.fn(),api=new CollectionGuestUploadsAPI({request},tenant,collection),controller=new AbortController();controller.abort();
 await expect(api.list(controller.signal)).rejects.toThrow();await expect(api.moderate(row,'reject',controller.signal)).rejects.toThrow();expect(request).not.toHaveBeenCalled();
});
