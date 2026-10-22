import {expect,it,vi} from 'vitest';
import {CollectionMetadataAPI,type CollectionMetadataPatch} from './collection-metadata-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const row=()=>({id:id(3),tenant_id:id(1),project_id:id(2),name:'Wedding',description:'Photos',state:'draft',revision:'1',sort_mode:'manual',created_at:'2026-10-17T10:00:00Z'} as const);
const patch:CollectionMetadataPatch={name:'Updated',description:'New',sort_mode:'filename',cover_item_id:id(4)};
it('sends exact If-Match, checks the frozen current gallery cover and actual incremented response',async()=>{
 const request=vi.fn().mockResolvedValue({...row(),...patch,revision:'2'}),api=new CollectionMetadataAPI({request} as any,id(1),id(2),id(3));await api.update(row(),patch,[id(4)],new AbortController().signal);expect(request.mock.calls[0][1]).toMatchObject({method:'PATCH',headers:{'If-Match':'"1"'},body:patch});request.mockClear();await expect(api.update(row(),patch,[id(8)],new AbortController().signal)).rejects.toThrow();expect(request).not.toHaveBeenCalled();
});
it('keeps full int64 PATCH revisions exact and refuses unsafe legacy numeric DELETE bodies',async()=>{
 const request=vi.fn().mockResolvedValue({...row(),...patch,revision:'9223372036854775807'}),api=new CollectionMetadataAPI({request} as any,id(1),id(2),id(3)),current={...row(),revision:'9223372036854775806'};await api.update(current,patch,[id(4)],new AbortController().signal);expect(request.mock.calls[0][1].headers['If-Match']).toBe('"9223372036854775806"');request.mockClear();await expect(api.archive(current,new AbortController().signal)).rejects.toThrow();expect(request).not.toHaveBeenCalled();
});
it('honors the actual asynchronous archive request and never claims physical deletion',async()=>{
 const request=vi.fn().mockResolvedValue({job_id:id(9),status:'queued'}),api=new CollectionMetadataAPI({request} as any,id(1),id(2),id(3));expect(await api.archive(row(),new AbortController().signal)).toEqual({job_id:id(9),status:'queued'});expect(request.mock.calls[0][1]).toMatchObject({method:'DELETE',body:{expected_revision:1,confirm_resource_id:id(3)}});request.mockResolvedValue({job_id:id(9),status:'deleted'});await expect(api.archive(row(),new AbortController().signal)).rejects.toThrow();
});
it('rejects wrong tenant, forged resource, changed response policy and preaborted or late cancelled reads',async()=>{
 const request=vi.fn().mockResolvedValue({...row(),tenant_id:id(8)}),api=new CollectionMetadataAPI({request} as any,id(1),id(2),id(3));await expect(api.get(new AbortController().signal)).rejects.toThrow();const c=new AbortController();c.abort();request.mockClear();await expect(api.get(c.signal)).rejects.toThrow();expect(request).not.toHaveBeenCalled();let resolve!:(v:unknown)=>void;request.mockImplementation(()=>new Promise(done=>resolve=done));const late=new AbortController(),pending=api.get(late.signal);late.abort();resolve(row());await expect(pending).rejects.toThrow();
});
it('checks a pending PATCH against its original state and detached requested values',async()=>{
 let finish!:(value:unknown)=>void;
 const request=vi.fn((_path:string,_options:unknown)=>new Promise(resolve=>{finish=resolve;}));
 const api=new CollectionMetadataAPI({request} as any,id(1),id(2),id(3));
 const current={...row(),state:'draft' as 'draft'|'published'},draft={...patch};
 const pending=api.update(current,draft,[id(4)],new AbortController().signal);
 current.state='published';draft.name='Later edit';
 expect(request.mock.calls[0][1]).toMatchObject({body:patch,headers:{'If-Match':'"1"'}});
 finish({...row(),...patch,revision:'2'});
 await expect(pending).resolves.toMatchObject({state:'draft',name:'Updated',revision:'2'});
});
it('refuses a runtime numeric revision before sending a mutation',async()=>{
 const request=vi.fn(),api=new CollectionMetadataAPI({request} as any,id(1),id(2),id(3));
 await expect(api.update({...row(),revision:1} as any,patch,[id(4)],new AbortController().signal)).rejects.toThrow('COLLECTION_PATCH_INVALID');
 expect(request).not.toHaveBeenCalled();
});
it('publishes only the collection with exact CAS and checks the resulting state',async()=>{
 const request=vi.fn().mockResolvedValue({...row(),state:'published',revision:'2'}),api=new CollectionMetadataAPI({request} as any,id(1),id(2),id(3));
 await api.publishCollection(row(),true,new AbortController().signal);
 expect(request.mock.calls[0]).toMatchObject([expect.stringContaining('/publication'),{method:'POST',body:{expected_revision:1,item_ids:[],publish:true,publish_collection:true}}]);
 request.mockResolvedValue({...row(),revision:'2'});await expect(api.publishCollection(row(),true,new AbortController().signal)).rejects.toThrow('COLLECTION_PUBLICATION_RECEIPT_INVALID');
 request.mockClear();await expect(api.publishCollection({...row(),revision:'9007199254740992'},true,new AbortController().signal)).rejects.toThrow('COLLECTION_PUBLICATION_INVALID');expect(request).not.toHaveBeenCalled();
});
it('refuses publication replies that silently change collection metadata',async()=>{
 const request=vi.fn(),api=new CollectionMetadataAPI({request} as any,id(1),id(2),id(3));
 for(const changed of [{name:'Other'},{description:'Other'},{sort_mode:'filename'},{cover_item_id:id(8)}]){
  request.mockResolvedValue({...row(),state:'published',revision:'2',...changed});await expect(api.publishCollection(row(),true,new AbortController().signal)).rejects.toThrow('COLLECTION_PUBLICATION_RECEIPT_INVALID');
 }
});
