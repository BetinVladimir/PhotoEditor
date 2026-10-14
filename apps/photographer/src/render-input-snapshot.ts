import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import type {V2VersionBinding} from './catalog-types';
import {isRenderSourceBinding} from './render-batch';
export type RenderInputSnapshotRequest={sources:V2VersionBinding[]};
export type RenderInputSnapshotResult={id:string;digest:string;item_count:number};
export function renderInputSnapshotRequest(value:RenderInputSnapshotRequest):RenderInputSnapshotRequest{
 if(!value||typeof value!=='object'||Object.keys(value).length!==1||!Object.prototype.hasOwnProperty.call(value,'sources')||!Array.isArray(value.sources)||value.sources.length<1||value.sources.length>5000)throw new Error('RENDER_INPUT_INVALID');
 const seen=new Set<string>();
 const sources=value.sources.map(source=>{
  if(!isRenderSourceBinding(source)||seen.has(source.asset_version_id.toLowerCase()))throw new Error('RENDER_INPUT_INVALID');
  seen.add(source.asset_version_id.toLowerCase());
  return {asset_version_id:source.asset_version_id.toLowerCase(),source:{artifact_id:source.source.artifact_id.toLowerCase(),sha256:source.source.sha256},canvas_hash:source.canvas_hash};
 });
 return {sources};
}
export function renderInputSnapshotDigest(request:RenderInputSnapshotRequest):string{
 return bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(renderInputSnapshotRequest(request).sources))));
}
export function renderInputSnapshotResult(value:unknown,digest:string,count:number):RenderInputSnapshotResult{
 if(!value||typeof value!=='object'||Array.isArray(value))throw new Error('RENDER_INPUT_RESULT_INVALID');
 const v=value as Record<string,unknown>;
 if(Object.keys(v).length!==3||!['id','digest','item_count'].every(k=>Object.prototype.hasOwnProperty.call(v,k))||typeof v.id!=='string'||!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v.id)||v.id==='00000000-0000-0000-0000-000000000000'||v.digest!==digest||v.item_count!==count||count<1||count>5000)throw new Error('RENDER_INPUT_RESULT_INVALID');
 return {id:v.id,digest,item_count:count};
}
