import type {Collection,Page} from './catalog-types';
import {readCreatedCollection} from './collection-create-response';
export async function projectCollectionPages(project:string,read:(cursor?:string)=>Promise<Page<Collection>>,signal:AbortSignal,tenant?:string){
 const result:Collection[]=[],ids=new Set<string>(),cursors=new Set<string>();let cursor:string|undefined;
 do{
  signal.throwIfAborted();const page=await read(cursor);signal.throwIfAborted();
  if(!Array.isArray(page.items)||page.items.length>100)throw new Error('PROJECT_COLLECTIONS_INVALID');
  for(const raw of page.items){const item=tenant===undefined?raw:readCreatedCollection(raw,tenant,project);if(!item.id||item.project_id!==project||ids.has(item.id))throw new Error('PROJECT_COLLECTIONS_INVALID');ids.add(item.id);result.push(item);}
  const next=page.next_cursor;if(next!=null&&next!==''){if(typeof next!=='string'||cursors.has(next)||!page.items.length)throw new Error('PROJECT_COLLECTIONS_INVALID');cursors.add(next);cursor=next;}else cursor=undefined;
 }while(cursor);return result;
}
