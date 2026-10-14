import type {MultiOperation} from './multi-intent';
import {readMultiReplacementScope,type MultiReplacementResultScope} from './multi-replacement-result';
export type MultiReplacementPage={review:string;decisionHash:string;items:Array<{originalTask:string;project:string;operation:MultiOperation;scope:MultiReplacementResultScope|null}>;next:string|null};
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const fields=(v:unknown,keys:string[]):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.hasOwn(v,k));
export function readMultiReplacementPage(value:unknown,tenant:string,review:string,decisionHash:string,after:string|null):MultiReplacementPage {
 const invalid=():never=>{throw new Error('MULTI_REPLACEMENT_PAGE_INVALID');};
 if(!uuid(tenant)||!uuid(review)||!/^[0-9a-f]{64}$/.test(decisionHash)||(after!==null&&!uuid(after))||!fields(value,['review_id','decision_hash','items','next_after']))return invalid();
 if(value.review_id!==review||value.decision_hash!==decisionHash||!Array.isArray(value.items)||value.items.length>100)return invalid();
 let previous=after;
 const items=value.items.map(item=>{
  if(!fields(item,['original_task_id','project_id','operation','scope'])||(item.operation!=='optical.panorama'&&item.operation!=='optical.focus_stack')||!uuid(item.original_task_id)||!uuid(item.project_id)||(previous!==null&&item.original_task_id<=previous))return invalid();
  previous=item.original_task_id;
  return {originalTask:item.original_task_id,project:item.project_id,operation:item.operation as MultiOperation,scope:item.scope===null?null:readMultiReplacementScope(item.scope,tenant,item.project_id,review,item.original_task_id,decisionHash,item.operation as MultiOperation)};
 });
 if(value.next_after!==null&&(!uuid(value.next_after)||items.length!==100||value.next_after!==previous))return invalid();
 return {review,decisionHash,items,next:value.next_after as string|null};
}
