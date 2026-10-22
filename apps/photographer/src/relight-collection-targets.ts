import {freezeCollectionVersionIntent,type CollectionVersionSelection} from './collection-version';
export type RelightCollectionTarget=Omit<CollectionVersionSelection,'version'>&{name:string};
export function readRelightCollectionTargets(value:unknown):{items:RelightCollectionTarget[];next_cursor:string|null}{
 const fail=():never=>{throw new Error('RELIGHT_COLLECTION_TARGETS_INVALID')};
 if(!value||typeof value!=='object'||Array.isArray(value)||Object.keys(value).sort().join(',')!=='items,next_cursor')return fail();
 const v=value as {items:unknown[];next_cursor:unknown};
 if(!Array.isArray(v.items)||v.items.length>50)return fail();
 const seen=new Set<string>();
 const items=v.items.map(raw=>{
  if(!raw||typeof raw!=='object'||Array.isArray(raw)||Object.keys(raw).sort().join(',')!=='asset,collection,expectedVersion,item,name,revision')return fail();
  const target=raw as RelightCollectionTarget,intent=freezeCollectionVersionIntent(target);
  if(typeof target.name!=='string'||target.name.length>1024||seen.has(intent.item))return fail();seen.add(intent.item);
  return {...intent,name:target.name};
 });
 if(v.next_cursor!==null&&(items.length!==50||v.next_cursor!==items[49].item))return fail();
 return {items,next_cursor:v.next_cursor as string|null};
}
