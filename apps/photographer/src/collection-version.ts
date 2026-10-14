export type CollectionVersionSelection={collection:string;item:string;asset:string;revision:string;expectedVersion:string;version:string};
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
export function freezeCollectionVersionIntent(value:Omit<CollectionVersionSelection,'version'>):Omit<CollectionVersionSelection,'version'>{
 if(!value||![value.collection,value.item,value.asset,value.expectedVersion].every(uuid)||typeof value.revision!=='string'||!/^[1-9][0-9]{0,18}$/.test(value.revision)||BigInt(value.revision)>9223372036854775807n)throw new Error('COLLECTION_VERSION_INVALID');
 return {collection:value.collection,item:value.item,asset:value.asset,revision:value.revision,expectedVersion:value.expectedVersion};
}
export function freezeCollectionVersion(value:CollectionVersionSelection):CollectionVersionSelection{
 const intent=freezeCollectionVersionIntent(value);
 if(!uuid(value.version)||value.version===intent.expectedVersion)throw new Error('COLLECTION_VERSION_INVALID');
 return {...intent,version:value.version};
}
export function readCollectionVersionSelection(value:unknown,expected:CollectionVersionSelection):{id:string;collection_id:string;asset_id:string;asset_version_id:string}{
 const scope=freezeCollectionVersion(expected);
 if(!value||typeof value!=='object'||Array.isArray(value))throw new Error('COLLECTION_VERSION_RESULT_INVALID');
 const v=value as Record<string,unknown>;
 if(v.id!==scope.item||v.collection_id!==scope.collection||v.asset_id!==scope.asset||v.asset_version_id!==scope.version)throw new Error('COLLECTION_VERSION_RESULT_INVALID');
 return {id:scope.item,collection_id:scope.collection,asset_id:scope.asset,asset_version_id:scope.version};
}
export function reconcileCollectionVersion(value:unknown,expected:CollectionVersionSelection):'applied'|'unchanged'|'conflict'{
 const scope=freezeCollectionVersion(expected);
 if(!value||typeof value!=='object'||Array.isArray(value))throw new Error('COLLECTION_VERSION_STATE_INVALID');
 const state=value as {item?:Record<string,unknown>;collection_revision?:unknown};
 if(typeof state.collection_revision!=='string'||!/^[1-9][0-9]{0,18}$/.test(state.collection_revision)||BigInt(state.collection_revision)>9223372036854775807n||!state.item||state.item.id!==scope.item||state.item.collection_id!==scope.collection||state.item.asset_id!==scope.asset)throw new Error('COLLECTION_VERSION_STATE_INVALID');
 if(state.item.asset_version_id===scope.version)return 'applied';
 if(state.item.asset_version_id===scope.expectedVersion&&state.collection_revision===scope.revision)return 'unchanged';
 return 'conflict';
}
