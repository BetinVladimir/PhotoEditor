import {it,expect} from 'vitest';
import {readPerspectiveReplacementPage} from './perspective-replacement-page';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`,hash='d'.repeat(64);
const page=()=>({review_id:id(1),decision_hash:hash,items:[{original_task_id:id(3),project_id:id(2),scope:null}],next_after:null});
it('retains unavailable entries without inventing admitted scope',()=>{
 const body=page(),owned=readPerspectiveReplacementPage(body,id(9),id(1),hash,null);body.items[0].original_task_id=id(5);
 expect(owned.items).toEqual([{originalTask:id(3),project:id(2),scope:null}]);
});
it('rejects foreign decisions, unordered duplicates and invalid continuation',()=>{
 const good=page();
 for(const bad of [{...good,unknown:true},{...good,review_id:id(2)},{...good,decision_hash:'e'.repeat(64)},{...good,items:[...good.items,...good.items]},{...good,next_after:id(3)},{...good,items:[{...good.items[0],scope:{}}]}])expect(()=>readPerspectiveReplacementPage(bad,id(9),id(1),hash,null)).toThrow();
 expect(()=>readPerspectiveReplacementPage(good,id(9),id(1),hash,id(3))).toThrow();
});
