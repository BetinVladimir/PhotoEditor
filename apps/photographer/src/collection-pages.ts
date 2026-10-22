import type {Collection,CollectionItem,Page} from './catalog-types';
/** Fetch metadata only, in revision-consistent pages of at most 100. */
export async function loadCollectionPages(collection:Collection,read:(cursor?:string)=>Promise<Page<CollectionItem>>,signal:AbortSignal):Promise<CollectionItem[]>{
 const items:CollectionItem[]=[],ids=new Set<string>(),cursors=new Set<string>();let cursor:string|undefined;
 if(!collection.id||!/^\d+$/.test(collection.revision))throw new Error('COLLECTION_PAGE_INVALID');
 do{
  signal.throwIfAborted();const page=await read(cursor);signal.throwIfAborted();
  if(page.revision!==collection.revision||!Array.isArray(page.items)||page.items.length>100)throw new Error('COLLECTION_PAGE_CHANGED');
  for(const item of page.items){if(item.collection_id!==collection.id||!item.id||ids.has(item.id)||items.length>=5000)throw new Error('COLLECTION_PAGE_INVALID');ids.add(item.id);items.push({...item,tags:[...item.tags]});}
  const next=page.next_cursor;
  if(next!=null&&next!==''){
   if(typeof next!=='string'||next.length>4096||cursors.has(next)||page.items.length===0||items.length>=5000)throw new Error('COLLECTION_PAGE_INVALID');cursors.add(next);cursor=next;
  }else cursor=undefined;
 }while(cursor);
 return items;
}
