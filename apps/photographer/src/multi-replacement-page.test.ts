import {it,expect} from 'vitest';
import {readMultiReplacementPage} from './multi-replacement-page';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const hash='a'.repeat(64);
const item=(n:number,operation='optical.panorama')=>({original_task_id:id(n),project_id:id(2),operation,scope:null});
const page=(items:unknown[],next_after:string|null=null)=>({review_id:id(3),decision_hash:hash,items,next_after});
const parse=(v:unknown,after:string|null=null)=>readMultiReplacementPage(v,id(1),id(3),hash,after);
it('keeps unavailable nodes for both operations and owns the returned page',()=>{
 const input=page([item(10),item(11,'optical.focus_stack')]),out=parse(input);
 expect(out.items.map(v=>v.operation)).toEqual(['optical.panorama','optical.focus_stack']);
 expect(out.items.every(v=>v.scope===null)).toBe(true);input.items.length=0;expect(out.items).toHaveLength(2);
});
it('rejects unsupported operation, duplicates, foreign decisions and unknown fields',()=>{
 for(const value of [page([item(10,'optical.perspective')]),page([item(10),item(10)]),page([item(11),item(10)]),{...page([]),decision_hash:'b'.repeat(64)},{...page([]),extra:true},page([{...item(10),extra:true}])])expect(()=>parse(value)).toThrow();
});
it('requires a full page and the exact last item for continuation',()=>{
 const items=Array.from({length:100},(_,i)=>item(i+10));expect(parse(page(items,id(109))).next).toBe(id(109));
 expect(()=>parse(page(items,id(108)))).toThrow();expect(()=>parse(page([item(10)],id(10)))).toThrow();expect(()=>parse(page([item(10)]),id(10))).toThrow();
});
