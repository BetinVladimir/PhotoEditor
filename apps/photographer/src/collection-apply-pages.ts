import type {Page} from './catalog-types';
// Refuse oversized/inconsistent catalogues rather than displaying a partial list.
export async function loadApplyCatalogPages<T extends {id:string}>(read:(cursor?:string)=>Promise<Page<T>>,active:()=>boolean):Promise<T[]>{
 const result:T[]=[],ids=new Set<string>(),cursors=new Set<string>();let cursor:string|undefined;
 do{
  if(!active())throw new Error('COLLECTION_APPLY_SCOPE_CHANGED');
  const page=await read(cursor);
  if(!active())throw new Error('COLLECTION_APPLY_SCOPE_CHANGED');
  if(!page||!Array.isArray(page.items)||result.length+page.items.length>5000)throw new Error('COLLECTION_APPLY_CATALOG_INVALID');
  for(const item of page.items){if(!item||typeof item.id!=='string'||!item.id||ids.has(item.id))throw new Error('COLLECTION_APPLY_CATALOG_INVALID');ids.add(item.id);result.push(item)}
  cursor=page.next_cursor??undefined;
  if(cursor!==undefined){if(typeof cursor!=='string'||!cursor||cursor.length>2048||cursors.has(cursor)||cursors.size>=5000)throw new Error('COLLECTION_APPLY_CATALOG_INVALID');cursors.add(cursor)}
 }while(cursor!==undefined);
 return result;
}
