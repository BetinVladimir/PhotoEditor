import {expect,it,vi} from 'vitest';
import {freezeGalleryFilter,loadCollectionGallery} from './collection-gallery-api';
import type {Collection} from './catalog-types';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const collection={id:id(3),tenant_id:id(2),project_id:id(9),name:'Gallery',description:'',state:'draft',sort_mode:'manual',created_at:'2026-10-17T00:00:00Z',revision:'7'} as Collection;
const filter={search:' wedding ',review:'pending_review',visibility:'hidden',rating:'5',flag:'pick',tag:'portrait',section_id:''};
const item={id:id(4),collection_id:id(3),asset_id:id(5),asset_version_id:id(6),position:0,visibility:'hidden',moderation_state:'pending_review',rating:5,flag:'pick',tags:['portrait'],created_at:'2026-10-17T12:00:00Z'};
it('sends actual server filters through all revision-fenced pages and rechecks metadata',async()=>{
 const request=vi.fn().mockResolvedValueOnce({items:[item],revision:'7',next_cursor:'next'}).mockResolvedValueOnce({items:[{...item,id:id(7)}],revision:'7',next_cursor:null}).mockResolvedValueOnce(collection);
 expect(await loadCollectionGallery({request} as never,id(2),collection,filter,new AbortController().signal)).toHaveLength(2);
 expect(request.mock.calls[0][0]).toContain('search=wedding&review=pending_review&visibility=hidden&rating=5&flag=pick&tag=portrait');expect(request.mock.calls[1][0]).toContain('cursor=next');expect(request.mock.calls[2][0]).not.toContain('/items');
});
it('refuses revision change, substituted membership and unsafe position',async()=>{
 for(const raw of [{...item,collection_id:id(8)},{...item,position:9007199254740992}])await expect(loadCollectionGallery({request:vi.fn().mockResolvedValue({items:[raw],revision:'7'})} as never,id(2),collection,filter,new AbortController().signal)).rejects.toThrow();
 const request=vi.fn().mockResolvedValueOnce({items:[item],revision:'7'}).mockResolvedValueOnce({...collection,revision:'8'});await expect(loadCollectionGallery({request} as never,id(2),collection,filter,new AbortController().signal)).rejects.toThrow('CHANGED');
});
it('normalizes search and refuses unsupported decisions/oversize search',()=>{expect(freezeGalleryFilter(filter).search).toBe('wedding');for(const bad of [{...filter,review:'ready'},{...filter,search:'x'.repeat(201)},{...filter,rating:'01'}])expect(()=>freezeGalleryFilter(bad)).toThrow();});
it('refuses a substituted project in the final metadata fence',async()=>{
 const request=vi.fn().mockResolvedValueOnce({items:[item],revision:'7'}).mockResolvedValueOnce({...collection,project_id:id(8)});
 await expect(loadCollectionGallery({request} as never,id(2),collection,filter,new AbortController().signal)).rejects.toThrow('COLLECTION_INVALID');
});
it('refuses cross-tenant scope before transport and oversized cursors before another page',async()=>{
 const request=vi.fn();await expect(loadCollectionGallery({request} as never,id(8),collection,filter,new AbortController().signal)).rejects.toThrow('GALLERY_SCOPE_INVALID');expect(request).not.toHaveBeenCalled();
 request.mockResolvedValue({items:[item],revision:'7',next_cursor:'x'.repeat(4097)});await expect(loadCollectionGallery({request} as never,id(2),collection,filter,new AbortController().signal)).rejects.toThrow('COLLECTION_PAGE_INVALID');expect(request).toHaveBeenCalledTimes(1);
});
it('refuses permissive date shortcuts in gallery item wire values',async()=>{
 const request=vi.fn().mockResolvedValue({items:[{...item,created_at:'1'}],revision:'7'});
 await expect(loadCollectionGallery({request} as never,id(2),collection,filter,new AbortController().signal)).rejects.toThrow('GALLERY_ITEM_INVALID');
});
