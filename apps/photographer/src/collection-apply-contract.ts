import type {ApplyAccepted,Collection} from './catalog-types';
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
export function collectionApplyRequest(collection:Pick<Collection,'id'|'revision'>,version:string,strength:number,name:string,key:string){
 if(!collection||!uuid(collection.id)||!uuid(version)||typeof collection.revision!=='string'||collection.revision.length>16||! /^[1-9][0-9]*$/.test(collection.revision)||BigInt(collection.revision)>9007199254740991n||!Number.isFinite(strength)||strength<0||strength>1||typeof name!=='string'||name.length>240||Array.from(name.trim()).length>120||typeof key!=='string'||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('COLLECTION_APPLY_REQUEST_INVALID');
 return {source_revision:Number(collection.revision),style_version_id:version,scope:'all' as const,strength,output_name:name.trim()};
}
export function readCollectionApplyAccepted(value:unknown):ApplyAccepted{
 if(!value||typeof value!=='object'||Array.isArray(value)||Object.keys(value).sort().join(',')!=='output_collection_id,run_id,status')throw new Error('COLLECTION_APPLY_RECEIPT_INVALID');
 const v=value as ApplyAccepted;
 if(!uuid(v.run_id)||!uuid(v.output_collection_id)||v.status!=='queued')throw new Error('COLLECTION_APPLY_RECEIPT_INVALID');
 return {run_id:v.run_id,output_collection_id:v.output_collection_id,status:v.status};
}
