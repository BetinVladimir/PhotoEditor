import {readCreatedCollection} from './collection-create-response';
import type {ApiClient} from '@photoeditor/api-client';
import type {Collection,CollectionItem} from './catalog-types';
import {loadCollectionPages} from './collection-pages';
import {bulkUUID} from './collection-bulk-api';
export type GalleryFilter={search:string;review:string;visibility:string;rating:string;flag:string;tag:string;section_id:string};
export function freezeGalleryFilter(raw:GalleryFilter):GalleryFilter{
 const v={...raw,search:raw.search.trim()};
 if(Object.keys(v).sort().join(',')!=='flag,rating,review,search,section_id,tag,visibility'||Object.values(v).some(x=>typeof x!=='string')||Array.from(v.search).length>200||Array.from(v.tag).length>64||!['','pending_review','accepted','rejected'].includes(v.review)||!['','hidden','visible'].includes(v.visibility)||!['','0','1','2','3','4','5'].includes(v.rating)||!['','none','pick','reject'].includes(v.flag)||v.section_id!==''&&!bulkUUID(v.section_id))throw Error('GALLERY_FILTER_INVALID');return v;
}
export function readGalleryItem(raw:unknown,collection:string):CollectionItem{
 if(!raw||typeof raw!=='object'||Array.isArray(raw))throw Error('GALLERY_ITEM_INVALID');const v=raw as Record<string,unknown>;
 if(Object.keys(v).some(k=>!['id','collection_id','asset_id','asset_version_id','section_id','position','visibility','moderation_state','rating','flag','color_label','tags','created_at'].includes(k))||!bulkUUID(v.id)||v.collection_id!==collection||!bulkUUID(v.asset_id)||!bulkUUID(v.asset_version_id)||v.section_id!==undefined&&!bulkUUID(v.section_id)||!Number.isSafeInteger(v.position)||Number(v.position)<0||!['hidden','visible'].includes(v.visibility as string)||!['pending_review','accepted','rejected'].includes(v.moderation_state as string)||!Number.isInteger(v.rating)||Number(v.rating)<0||Number(v.rating)>5||!['none','pick','reject'].includes(v.flag as string)||v.color_label!==undefined&&!['Red','Yellow','Green','Blue','Purple'].includes(v.color_label as string)||!Array.isArray(v.tags)||v.tags.length>100||v.tags.some(t=>typeof t!=='string'||!t.trim()||Array.from(t).length>64)||new Set(v.tags).size!==v.tags.length||typeof v.created_at!=='string'||!/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?(?:Z|[+-]\d\d:\d\d)$/.test(v.created_at)||!Number.isFinite(Date.parse(v.created_at)))throw Error('GALLERY_ITEM_INVALID');
 return {...v,tags:[...v.tags]} as CollectionItem;
}
export async function loadCollectionGallery(client:Pick<ApiClient,'request'>,tenant:string,collection:Collection,raw:GalleryFilter,signal:AbortSignal){
 signal.throwIfAborted();if(!bulkUUID(tenant)||!bulkUUID(collection.id)||collection.tenant_id!==tenant||!bulkUUID(collection.project_id))throw Error('GALLERY_SCOPE_INVALID');const filter=freezeGalleryFilter(raw),id=collection.id,revision=collection.revision,project=collection.project_id;
 const query=Object.entries(filter).filter(([,v])=>v!=='').map(([k,v])=>`${k}=${encodeURIComponent(v)}`).join('&');
 const items=await loadCollectionPages({id,revision} as Collection,async cursor=>{
  const p=await client.request<unknown>(`/api/v1/tenants/${tenant}/collections/${id}/items?limit=100${query?'&'+query:''}${cursor?'&cursor='+encodeURIComponent(cursor):''}`,{signal});signal.throwIfAborted();
  if(!p||typeof p!=='object'||Array.isArray(p))throw Error('GALLERY_PAGE_INVALID');const page=p as Record<string,unknown>;
  if(!Array.isArray(page.items)||Object.keys(page).some(k=>!['items','next_cursor','revision'].includes(k)))throw Error('GALLERY_PAGE_INVALID');
  return {revision:page.revision as string,next_cursor:page.next_cursor as string|null|undefined,items:page.items.map(v=>readGalleryItem(v,id))};
 },signal);
 const latest=await client.request<unknown>(`/api/v1/tenants/${tenant}/collections/${id}`,{signal});signal.throwIfAborted();
 const current=readCreatedCollection(latest,tenant,project);
 if(current.id!==id||current.revision!==revision)throw Error('COLLECTION_PAGE_CHANGED');
 return items;
}
