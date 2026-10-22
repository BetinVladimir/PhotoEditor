import type {ApiClient} from '@photoeditor/api-client';
import {scopeUUID} from './workspace-access';
export type CollectionGroupItem={collection_item_id:string;item_id:string;asset_id:string;asset_version_id:string;scene_id:string;exact_group_id:string;burst_group_id:string;rank:number;score:number;recommended:boolean;manual:boolean;eye_review:boolean;eye_state:'open'|'closed'|'unknown'};
export type CollectionGroupContext={review_id:string;run_id:string;candidate_hash:string;state:'pending'|'decided';total:number;matched:number;unmatched:number;items:CollectionGroupItem[]|null};
export type CollectionGroupPage={collection_id:string;revision:string;contexts:CollectionGroupContext[];next_cursor:string|null};
const object=(raw:unknown,keys:string[])=>{if(!raw||typeof raw!=='object'||Array.isArray(raw)||Object.keys(raw).length!==keys.length||keys.some(k=>!Object.hasOwn(raw,k)))throw Error('COLLECTION_GROUPS_INVALID');return raw as Record<string,unknown>};
const count=(n:unknown)=>Number.isInteger(n)&&Number(n)>=0&&Number(n)<=5000;
export function readCollectionGroups(raw:unknown,collection:string,revision:string,selected?:string):CollectionGroupPage{
 const p=object(raw,['collection_id','revision','contexts','next_cursor']);if(p.collection_id!==collection||p.revision!==revision||!Array.isArray(p.contexts)||p.contexts.length>10||p.next_cursor!==null&&(typeof p.next_cursor!=='string'||!p.next_cursor.length||p.next_cursor.length>2048)||selected&&(p.contexts.length!==1||p.next_cursor!==null))throw Error('COLLECTION_GROUPS_INVALID');
 const reviews=new Set<string>();
 for(const rawContext of p.contexts){const c=object(rawContext,['review_id','run_id','candidate_hash','state','total','matched','unmatched','items']);
  if(!scopeUUID(c.review_id)||reviews.has(c.review_id)||!scopeUUID(c.run_id)||selected&&c.review_id!==selected||typeof c.candidate_hash!=='string'||!/^[0-9a-f]{64}$/.test(c.candidate_hash)||!['pending','decided'].includes(c.state as string)||!count(c.total)||Number(c.total)<1||!count(c.matched)||!count(c.unmatched)||Number(c.matched)+Number(c.unmatched)!==c.total)throw Error('COLLECTION_GROUPS_INVALID');reviews.add(c.review_id);
  if(!selected){if(c.items!==null)throw Error('COLLECTION_GROUPS_INVALID');continue;}
  if(!Array.isArray(c.items)||c.items.length!==c.matched)throw Error('COLLECTION_GROUPS_INVALID');const seen=new Set<string>();
  for(const rawItem of c.items){const item=object(rawItem,['collection_item_id','item_id','asset_id','asset_version_id','scene_id','exact_group_id','burst_group_id','rank','score','recommended','manual','eye_review','eye_state']);
   if(![item.collection_item_id,item.item_id,item.asset_id,item.asset_version_id].every(scopeUUID)||seen.has(item.collection_item_id as string)||['scene_id','exact_group_id','burst_group_id'].some(k=>typeof item[k]!=='string'||!(item[k] as string).length||(item[k] as string).length>80)||!Number.isInteger(item.rank)||Number(item.rank)<1||Number(item.rank)>5000||typeof item.score!=='number'||!Number.isFinite(item.score)||item.score<0||item.score>1||['recommended','manual','eye_review'].some(k=>typeof item[k]!=='boolean')||!['open','closed','unknown'].includes(item.eye_state as string)||item.eye_state==='unknown'&&!item.eye_review)throw Error('COLLECTION_GROUPS_INVALID');seen.add(item.collection_item_id as string);
  }
 }
 return structuredClone(p) as CollectionGroupPage;
}
export async function collectionGroups(client:Pick<ApiClient,'request'>,tenant:string,collection:string,revision:string,signal:AbortSignal,options:{cursor?:string;review?:string}={}){
 signal.throwIfAborted();if(![tenant,collection].every(scopeUUID)||!/^[1-9][0-9]{0,18}$/.test(revision)||BigInt(revision)>9223372036854775807n||options.review!==undefined&&!scopeUUID(options.review)||options.cursor!==undefined&&(!options.cursor.length||options.cursor.length>2048)||options.review&&options.cursor)throw Error('COLLECTION_GROUPS_SCOPE_INVALID');
 const query=options.review?'?review_id='+encodeURIComponent(options.review):options.cursor?'?cursor='+encodeURIComponent(options.cursor):'';
 const raw=await client.request<unknown>(`/api/v1/tenants/${tenant}/collections/${collection}/culling-groups${query}`,{signal});signal.throwIfAborted();return readCollectionGroups(raw,collection,revision,options.review);
}
