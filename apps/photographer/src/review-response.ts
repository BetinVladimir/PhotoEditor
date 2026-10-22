import type {Page,Review} from './catalog-types';
import {ownRenderReviewBinding} from './review-binding';
const id=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const sha=(v:unknown)=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
const required=['id','run_id','kind','state','revision','candidate_hash','reasons','created_at'];
const optional=['decision','decision_route','decision_hash','candidate_ids','proposal','image','label_map','candidate','base'];
export function readReviewView(raw:unknown,expected?:{id?:string;run?:string;state?:string}):Review{
 const fail=():never=>{throw Error('REVIEW_RESPONSE_INVALID')};
 if(!raw||typeof raw!=='object'||Array.isArray(raw))return fail();const v=raw as Record<string,unknown>;
 if(required.some(k=>!Object.hasOwn(v,k))||Object.keys(v).some(k=>!required.includes(k)&&!optional.includes(k))||!id(v.id)||!id(v.run_id)||expected?.id!==undefined&&v.id!==expected.id||expected?.run!==undefined&&v.run_id!==expected.run||expected?.state&&v.state!==expected.state||typeof v.kind!=='string'||!v.kind||v.kind.length>64||!['pending','decided','cancelled'].includes(v.state as string)||!Number.isSafeInteger(v.revision)||Number(v.revision)<1||!sha(v.candidate_hash)||typeof v.created_at!=='string'||!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.test(v.created_at)||!Number.isFinite(Date.parse(v.created_at)))return fail();
 if(v.state==='decided'?!['accept','reject','skip'].includes(v.decision as string):v.decision!==undefined)return fail();
 if(v.decision_route!==undefined&&(typeof v.decision_route!=='string'||!v.decision_route)||v.decision_hash!==undefined&&!sha(v.decision_hash))return fail();
 if(v.candidate_ids!==undefined&&(!Array.isArray(v.candidate_ids)||v.candidate_ids.some(x=>typeof x!=='string'||!x)||new Set(v.candidate_ids).size!==v.candidate_ids.length))return fail();
 for(const key of ['proposal','image','label_map','candidate','base'])if(v[key]!==undefined){const r=v[key];if(!r||typeof r!=='object'||Array.isArray(r)||Object.keys(r).sort().join(',')!=='artifact_id,sha256'||!id((r as Record<string,unknown>).artifact_id)||!sha((r as Record<string,unknown>).sha256))return fail();}
 return structuredClone(v) as Review;
}
export function readReviewDetail(raw:unknown,review:string):Review{return ownRenderReviewBinding(readReviewView(raw,{id:review}));}
export function readReviewPage(raw:unknown,expected:{state?:string;run?:string}):Page<Review>{
 if(!raw||typeof raw!=='object'||Array.isArray(raw))throw Error('REVIEW_PAGE_INVALID');const v=raw as Record<string,unknown>;
 if(!Object.hasOwn(v,'next_cursor')||Object.keys(v).some(k=>!['items','next_cursor'].includes(k))||!Array.isArray(v.items)||v.items.length>100||v.next_cursor!==null&&(typeof v.next_cursor!=='string'||!v.next_cursor||v.next_cursor.length>2048))throw Error('REVIEW_PAGE_INVALID');
 const items=v.items.map(row=>readReviewView(row,expected));if(new Set(items.map(row=>row.id)).size!==items.length)throw Error('REVIEW_PAGE_INVALID');return {items,next_cursor:v.next_cursor as string|null|undefined};
}
export function readReviewDecision(raw:unknown,before:Review,decision:'accept'|'reject'|'skip'):Review{
 const value=readReviewView(raw,{id:before.id,run:before.run_id,state:'decided'});
 if(value.kind!==before.kind||value.candidate_hash!==before.candidate_hash||value.revision!==before.revision+1||value.decision!==decision||!value.decision_route||!sha(value.decision_hash))throw Error('REVIEW_RECEIPT_INVALID');return value;
}
