import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import type {ArtifactRef,V2VersionBinding} from './catalog-types';
export type InterchangeSnapshotItem={ordinal:number;binding:V2VersionBinding;working_image:ArtifactRef;captured_at:string|null;camera_hash:string|null;recipe:ArtifactRef};
export type InterchangeSnapshotRequest={items:InterchangeSnapshotItem[]};
export type InterchangeSnapshotReceipt={id:string;artifact:ArtifactRef;digest:string;item_count:number};
const fail=():never=>{throw new Error('INTERCHANGE_SNAPSHOT_INVALID');};
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const digest=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
function fields(v:unknown,keys:string[]):v is Record<string,unknown>{return !!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.prototype.hasOwnProperty.call(v,k));}
function ref(v:unknown):ArtifactRef{if(!fields(v,['artifact_id','sha256'])||!uuid(v.artifact_id)||!digest(v.sha256))return fail();return {artifact_id:v.artifact_id,sha256:v.sha256};}
const hash=(value:unknown)=>bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(value))));
export function freezeInterchangeSnapshot(value:unknown,key:string):InterchangeSnapshotRequest{
 if(typeof key!=='string'||!/^[\x21-\x7e]{1,128}$/.test(key)||!fields(value,['items'])||!Array.isArray(value.items)||value.items.length<1||value.items.length>5000)return fail();
 const seen=new Set<string>();
 const items=value.items.map((item:unknown,ordinal:number):InterchangeSnapshotItem=>{
  if(!fields(item,['ordinal','binding','working_image','captured_at','camera_hash','recipe'])||item.ordinal!==ordinal||!fields(item.binding,['asset_version_id','source','canvas_hash'])||!uuid(item.binding.asset_version_id)||!digest(item.binding.canvas_hash)||seen.has(item.binding.asset_version_id))return fail();
  seen.add(item.binding.asset_version_id);
  const source=ref(item.binding.source),working=ref(item.working_image),recipe=ref(item.recipe);
  if(canonicalJSON(source)!==canonicalJSON(working))return fail();
  if(item.camera_hash!==null&&!digest(item.camera_hash))return fail();
  if(item.captured_at!==null&&(typeof item.captured_at!=='string'||!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.test(item.captured_at)||!Number.isFinite(Date.parse(item.captured_at))))return fail();
  return {ordinal,binding:{asset_version_id:item.binding.asset_version_id,source,canvas_hash:item.binding.canvas_hash},working_image:working,recipe,captured_at:item.captured_at as string|null,camera_hash:item.camera_hash as string|null};
 });
 const request={items};if(new TextEncoder().encode(canonicalJSON(request)).byteLength>1048576)return fail();return request;
}
export function readInterchangeSnapshotReceipt(value:unknown,request:InterchangeSnapshotRequest):InterchangeSnapshotReceipt{
 const owned=freezeInterchangeSnapshot(request,'receipt-validation');
 if(!fields(value,['id','artifact','digest','item_count'])||!uuid(value.id)||value.item_count!==owned.items.length)return fail();
 const unsigned={schema_version:1,snapshot_id:value.id,items:owned.items},expectedDigest=hash(unsigned);
 if(value.digest!==expectedDigest)return fail();
 const artifact=ref(value.artifact);
 if(artifact.sha256!==hash({...unsigned,snapshot_digest:expectedDigest}))return fail();
 return {id:value.id,artifact,digest:expectedDigest,item_count:owned.items.length};
}
