import {expect,it,vi} from 'vitest';
import {projectCollectionPages} from './project-collection-pages';
import type {Collection} from './catalog-types';
const item=(id:string)=>({id,project_id:'project'} as Collection);
it('reads collections beyond the first page and forwards opaque cursor',async()=>{
 const read=vi.fn().mockResolvedValueOnce({items:[item('one')],next_cursor:'opaque'}).mockResolvedValueOnce({items:[item('two')]});
 expect((await projectCollectionPages('project',read,new AbortController().signal)).map(x=>x.id)).toEqual(['one','two']);expect(read.mock.calls[1]).toEqual(['opaque']);
});
it('rejects cross-project rows and duplicate pagination',async()=>{
 await expect(projectCollectionPages('other',async()=>({items:[item('one')]}),new AbortController().signal)).rejects.toThrow();
 await expect(projectCollectionPages('project',async()=>({items:[item('one')],next_cursor:'repeat'}),new AbortController().signal)).rejects.toThrow();
});
it('stops pagination when the project scope is cancelled',async()=>{
 const controller=new AbortController(),read=vi.fn(async()=>{controller.abort();return {items:[item('one')],next_cursor:'next'};});
 await expect(projectCollectionPages('project',read,controller.signal)).rejects.toThrow();expect(read).toHaveBeenCalledTimes(1);
});
