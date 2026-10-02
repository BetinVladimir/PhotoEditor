import {expect,it,vi} from 'vitest';
import type {Collection,CollectionItem} from './catalog-types';
import {saveCompareDecisions} from './compare-decisions-save';
const collection={id:'collection',revision:'1',source_run_id:'run',state:'ready'} as Collection;
const items=['a','b'].map(asset_version_id=>({id:asset_version_id,asset_version_id,asset_id:asset_version_id,collection_id:'collection',moderation_state:'pending_review'} as CollectionItem));
it('saves sequentially with CAS and frozen decisions',async()=>{
 const decisions={a:'accept',b:'reject'} as const;
 const save=vi.fn(async(c:Collection,item:CollectionItem,d:'accept'|'reject')=>{expect(c.revision).toBe(item.id==='a'?'1':'2');return {...item,moderation_state:d==='accept'?'accepted':'rejected'};});
 const result=await saveCompareDecisions(collection,items,decisions,save,new AbortController().signal);expect(result.collection.revision).toBe('3');expect(result.results.map(x=>x.status)).toEqual(['saved','saved']);expect(collection.revision).toBe('1');
});
it('stops new writes after an uncertain failure',async()=>{
 const save=vi.fn(async()=>{throw new Error('transport');});const out=await saveCompareDecisions(collection,items,{a:'accept',b:'reject'},save,new AbortController().signal);expect(save).toHaveBeenCalledTimes(1);expect(out.results.map(x=>x.status)).toEqual(['failed','not_attempted']);
});
it('acknowledges an in-flight commit before stopping after cancellation',async()=>{
 const controller=new AbortController();const save=vi.fn(async(_c:Collection,item:CollectionItem)=>{controller.abort();return {...item,moderation_state:'accepted'};});const out=await saveCompareDecisions(collection,items,{a:'accept',b:'reject'},save,controller.signal);expect(out.collection.revision).toBe('2');expect(out.results.map(x=>x.status)).toEqual(['saved','cancelled']);
});
it('rejects ineligible frames before any mutation',async()=>{
 const save=vi.fn();await expect(saveCompareDecisions(collection,[{...items[0],moderation_state:'accepted'},items[1]],{a:'accept'},save,new AbortController().signal)).rejects.toThrow();expect(save).not.toHaveBeenCalled();
});
