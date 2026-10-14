import type {ArtifactRef,V2VersionBinding} from './catalog-types';
export type MultiOperation='optical.panorama'|'optical.focus_stack';
type Common={schema_version:1;frames:V2VersionBinding[];reference_ordinal:number;max_pixels:number;calibration:ArtifactRef};
export type PanoramaParams=Common&{projection:'planar'|'cylindrical'};
export type FocusStackParams=Common&{motion_policy:'review'|'reject'};
export type MultiIntent={schema_version:1;snapshot_id:string;params:PanoramaParams|FocusStackParams};
export const multiUUID=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const sha=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
const fields=(v:unknown,keys:string[]):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.prototype.hasOwnProperty.call(v,k));
const ref=(v:unknown):v is ArtifactRef=>fields(v,['artifact_id','sha256'])&&multiUUID(v.artifact_id)&&sha(v.sha256);
/** Own all ordered frame bindings. Never sort frames: reference_ordinal binds their order. */
export function freezeMultiIntent(operation:MultiOperation,value:MultiIntent):MultiIntent{
 const invalid=():never=>{throw new Error('MULTI_INTENT_INVALID');};
 if(operation!=='optical.panorama'&&operation!=='optical.focus_stack')invalid();
 if(!fields(value,['schema_version','snapshot_id','params'])||value.schema_version!==1||!multiUUID(value.snapshot_id))invalid();
 const p=value.params,extra=operation==='optical.panorama'?'projection':'motion_policy';
 if(!fields(p,['schema_version','frames','reference_ordinal','max_pixels','calibration',extra])||p.schema_version!==1||!Array.isArray(p.frames)||p.frames.length<2||p.frames.length>(operation==='optical.panorama'?20:30)||!Number.isInteger(p.reference_ordinal)||p.reference_ordinal<0||p.reference_ordinal>=p.frames.length||!Number.isInteger(p.max_pixels)||p.max_pixels<1||p.max_pixels>200_000_000||!ref(p.calibration))invalid();
 if(operation==='optical.panorama'){
  if(!('projection' in p)||(p.projection!=='planar'&&p.projection!=='cylindrical'))invalid();
 }else if(!('motion_policy' in p)||(p.motion_policy!=='review'&&p.motion_policy!=='reject'))invalid();
 const versions=new Set<string>();
 for(const frame of p.frames){
  if(!fields(frame,['asset_version_id','source','canvas_hash'])||!multiUUID(frame.asset_version_id)||versions.has(frame.asset_version_id)||!ref(frame.source)||frame.source.artifact_id===p.calibration.artifact_id||!sha(frame.canvas_hash))invalid();
  versions.add(frame.asset_version_id);
 }
 return structuredClone(value);
}

export type MultiCapabilities={schema_version:1;tenant_id:string;project_id:string;operation:MultiOperation;calibration:ArtifactRef;max_pixels:number;frame_limit:number};
export function readMultiCapabilities(value:unknown,tenant:string,project:string,operation:MultiOperation):MultiCapabilities{
 if(!fields(value,['schema_version','tenant_id','project_id','operation','calibration','max_pixels','frame_limit'])||value.schema_version!==1||!multiUUID(tenant)||!multiUUID(project)||value.tenant_id!==tenant||value.project_id!==project||value.operation!==operation||!ref(value.calibration)||!Number.isInteger(value.max_pixels)||typeof value.max_pixels!=='number'||value.max_pixels<1||value.max_pixels>200_000_000||value.frame_limit!==(operation==='optical.panorama'?20:30)||(operation!=='optical.panorama'&&operation!=='optical.focus_stack'))throw new Error('MULTI_CAPABILITIES_INVALID');
 return structuredClone(value) as MultiCapabilities;
}
export function isMultiBinding(value:unknown):value is V2VersionBinding{
 return fields(value,['asset_version_id','source','canvas_hash'])&&multiUUID(value.asset_version_id)&&ref(value.source)&&sha(value.canvas_hash);
}
