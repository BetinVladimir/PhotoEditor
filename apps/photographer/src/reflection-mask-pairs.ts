import type {ArtifactRef,V2VersionBinding} from './catalog-types';

export type ReflectionMaskPairQuery={snapshot_id:string;base:V2VersionBinding;after:string|null};
export type ReflectionMaskPair={mask:ArtifactRef;protected:ArtifactRef;target_pixel:ArtifactRef;protected_pixel:ArtifactRef};
export type ReflectionMaskPairPage={items:ReflectionMaskPair[];next_cursor:string|null};
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const sha=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
const fields=(v:unknown,keys:string[]):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.prototype.hasOwnProperty.call(v,k));
const ref=(v:unknown):v is ArtifactRef=>fields(v,['artifact_id','sha256'])&&uuid(v.artifact_id)&&sha(v.sha256);
const binding=(v:unknown):v is V2VersionBinding=>fields(v,['asset_version_id','source','canvas_hash'])&&uuid(v.asset_version_id)&&ref(v.source)&&sha(v.canvas_hash);
export function freezeReflectionMaskPairQuery(v:ReflectionMaskPairQuery):ReflectionMaskPairQuery{
 if(!fields(v,['snapshot_id','base','after'])||!uuid(v.snapshot_id)||!binding(v.base)||v.after!==null&&!uuid(v.after))throw new Error('REFLECTION_MASK_PAIR_QUERY_INVALID');
 return structuredClone(v);
}

export function readReflectionMaskPairPage(v:unknown,query:ReflectionMaskPairQuery):ReflectionMaskPairPage{
 freezeReflectionMaskPairQuery(query);
 if(!fields(v,['items','next_cursor'])||!Array.isArray(v.items)||v.items.length>100||v.next_cursor!==null&&!uuid(v.next_cursor))throw new Error('REFLECTION_MASK_PAIR_PAGE_INVALID');
 let previous=query.after??'';
 for(const item of v.items){
  if(!fields(item,['mask','protected','target_pixel','protected_pixel'])||![item.mask,item.protected,item.target_pixel,item.protected_pixel].every(ref))throw new Error('REFLECTION_MASK_PAIR_PAGE_INVALID');
  const ids=[item.mask.artifact_id,item.protected.artifact_id,item.target_pixel.artifact_id,item.protected_pixel.artifact_id];
  if(new Set(ids).size!==ids.length||item.mask.artifact_id<=previous)throw new Error('REFLECTION_MASK_PAIR_PAGE_INVALID');
  previous=item.mask.artifact_id;
 }
 if(v.next_cursor!==null&&(v.items.length!==100||v.next_cursor!==previous))throw new Error('REFLECTION_MASK_PAIR_PAGE_INVALID');
 return structuredClone(v) as ReflectionMaskPairPage;
}

export async function collectReflectionMaskPairs(fetch:(query:ReflectionMaskPairQuery,signal?:AbortSignal)=>Promise<ReflectionMaskPairPage>,query:ReflectionMaskPairQuery,signal?:AbortSignal){
 let current=freezeReflectionMaskPairQuery(query);const items:ReflectionMaskPair[]=[];
 for(let i=0;i<256;i++){
  if(signal?.aborted)throw new Error('REFLECTION_MASK_PAIRS_CANCELLED');
  const page=readReflectionMaskPairPage(await fetch(current,signal),current);
  if(signal?.aborted)throw new Error('REFLECTION_MASK_PAIRS_CANCELLED');
  items.push(...page.items);if(page.next_cursor===null)return items;
  current={...current,after:page.next_cursor};
 }
 throw new Error('REFLECTION_MASK_PAIR_PAGE_LIMIT');
}
