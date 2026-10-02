import {expect,it,vi} from 'vitest';
import type {Collection,CollectionItem} from './catalog-types';
import {loadCollectionPages} from './collection-pages';
const collection={id:'c',revision:'7'} as Collection;
const item=(id:string)=>({id,collection_id:'c',tags:[]} as unknown as CollectionItem);
it('loads all 5000 metadata rows through bounded revision-consistent pages',async()=>{
 const read=vi.fn(async(cursor?:string)=>{const page=Number(cursor||0);return {revision:'7',items:Array.from({length:100},(_,i)=>item(String(page*100+i))),next_cursor:page<49?String(page+1):null};});
 const result=await loadCollectionPages(collection,read,new AbortController().signal);expect(result).toHaveLength(5000);expect(read).toHaveBeenCalledTimes(50);expect(result[4999].id).toBe('4999');
});
it('rejects changed revisions, duplicate IDs and repeated cursors',async()=>{
 for(const page of [{revision:'8',items:[item('a')]},{revision:'7',items:[item('a'),item('a')]},{revision:'7',items:[item('a')],next_cursor:'same'}])await expect(loadCollectionPages(collection,async()=>page,new AbortController().signal)).rejects.toThrow();
});
it('does not fetch another page after cancellation',async()=>{
 const controller=new AbortController();const read=vi.fn(async()=>{controller.abort();return {revision:'7',items:[item('a')],next_cursor:'next'};});await expect(loadCollectionPages(collection,read,controller.signal)).rejects.toThrow();expect(read).toHaveBeenCalledTimes(1);
});
