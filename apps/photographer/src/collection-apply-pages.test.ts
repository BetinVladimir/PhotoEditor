import {it,expect,vi} from 'vitest';
import {loadApplyCatalogPages} from './collection-apply-pages';
it('loads the complete catalogue in cursor order',async()=>{
 const read=vi.fn(async(cursor?:string)=>cursor?{items:[{id:'second'}],next_cursor:null}:{items:[{id:'first'}],next_cursor:'next'});
 expect(await loadApplyCatalogPages(read,()=>true)).toEqual([{id:'first'},{id:'second'}]);expect(read.mock.calls).toEqual([[undefined],['next']]);
});
it('refuses duplicate IDs, cyclic/unbounded/empty cursors and oversized catalogues',async()=>{
 for(const mode of ['duplicate','cycle','long','empty','oversized']){
  const read=vi.fn(async()=>({items:mode==='oversized'?Array.from({length:5001},(_,i)=>({id:String(i)})):mode==='duplicate'?[{id:'same'},{id:'same'}]:[{id:crypto.randomUUID()}],next_cursor:mode==='cycle'?'same':mode==='long'?'x'.repeat(2049):mode==='empty'?'':null}));
  await expect(loadApplyCatalogPages(read,()=>true)).rejects.toThrow('INVALID');expect(read.mock.calls.length).toBeLessThanOrEqual(2);
 }
});
it('never requests another page after scope changes while reading',async()=>{
 let active=true;const read=vi.fn(async()=>{active=false;return {items:[{id:'old'}],next_cursor:'next'}});
 await expect(loadApplyCatalogPages(read,()=>active)).rejects.toThrow('SCOPE_CHANGED');expect(read).toHaveBeenCalledTimes(1);
});
