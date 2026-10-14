import type {CollectionItem,V2VersionBinding} from './catalog-types';
import {isMultiBinding} from './multi-intent';
/** Asset binding endpoints return the current version: never silently replace a selected version. */
export async function loadMultiFrameBinding(api:{v2Binding:(asset:string,signal?:AbortSignal)=>Promise<V2VersionBinding>},item:CollectionItem,signal:AbortSignal){
 signal.throwIfAborted();
 const expected={asset:item.asset_id,version:item.asset_version_id};
 const binding=await api.v2Binding(expected.asset,signal);signal.throwIfAborted();
 if(!isMultiBinding(binding)||binding.asset_version_id!==expected.version)throw new Error('MULTI_SELECTION_SOURCE_CHANGED');
 return structuredClone(binding);
}
