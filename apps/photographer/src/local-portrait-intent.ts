import type {ArtifactRef,V2VersionBinding} from './catalog-types';

export type LocalHairParams={schema_version:1;base:V2VersionBinding;strength:number;mask:ArtifactRef;protected:ArtifactRef;hue_delta_degrees:number;saturation_factor:number;detail:number};
export type LocalClothingParams={schema_version:1;base:V2VersionBinding;strength:number;mask:ArtifactRef;protected:ArtifactRef;hue_delta_degrees:number;saturation_factor:number;wrinkle_strength:number};
export type LocalPortraitSelection={schema_version:1;snapshot_id:string;ordinal:number;operation:'portrait.hair';params:LocalHairParams;target_pixel:ArtifactRef;protected_pixel:ArtifactRef}
 |{schema_version:1;snapshot_id:string;ordinal:number;operation:'portrait.clothing';params:LocalClothingParams;target_pixel:ArtifactRef;protected_pixel:ArtifactRef};
export type LocalPortraitTicket={run_id:string;workflow_id:string;task_id:string;state:'queued'};
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const sha=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
const fields=(v:unknown,keys:string[]):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.prototype.hasOwnProperty.call(v,k));
const ref=(v:unknown):v is ArtifactRef=>fields(v,['artifact_id','sha256'])&&uuid(v.artifact_id)&&sha(v.sha256);
const binding=(v:unknown):v is V2VersionBinding=>fields(v,['asset_version_id','source','canvas_hash'])&&uuid(v.asset_version_id)&&ref(v.source)&&sha(v.canvas_hash);
const finite=(n:unknown,lo:number,hi:number)=>typeof n==='number'&&Number.isFinite(n)&&n>=lo&&n<=hi;

export function freezeLocalPortraitSelection(v:LocalPortraitSelection):LocalPortraitSelection{
 if(!fields(v,['schema_version','snapshot_id','ordinal','operation','params','target_pixel','protected_pixel'])||v.schema_version!==1||!uuid(v.snapshot_id)||!Number.isInteger(v.ordinal)||v.ordinal<0||v.ordinal>=5000||!ref(v.target_pixel)||!ref(v.protected_pixel))throw new Error('LOCAL_PORTRAIT_SELECTION_INVALID');
 const p=v.params;
 const hair=v.operation==='portrait.hair',clothing=v.operation==='portrait.clothing';
 if(!hair&&!clothing)throw new Error('LOCAL_PORTRAIT_SELECTION_INVALID');
 const keys=hair?['schema_version','base','strength','mask','protected','hue_delta_degrees','saturation_factor','detail']:['schema_version','base','strength','mask','protected','hue_delta_degrees','saturation_factor','wrinkle_strength'];
 if(!fields(p,keys)||p.schema_version!==1||!binding(p.base)||!ref(p.mask)||!ref(p.protected)||!finite(p.strength,0,1)||!finite(p.hue_delta_degrees,hair?-30:-180,hair?30:180)||!finite(p.saturation_factor,hair?0.7:0,hair?1.3:2)||!finite(hair?(p as LocalHairParams).detail:(p as LocalClothingParams).wrinkle_strength,0,1))throw new Error('LOCAL_PORTRAIT_SELECTION_INVALID');
 if(new Set([p.base.source.artifact_id,p.mask.artifact_id,p.protected.artifact_id,v.target_pixel.artifact_id,v.protected_pixel.artifact_id]).size!==5)throw new Error('LOCAL_PORTRAIT_SELECTION_INVALID');
 return structuredClone(v);
}
export function readLocalPortraitPrepared(v:unknown,selected:LocalPortraitSelection):LocalPortraitSelection{
 const prepared=freezeLocalPortraitSelection(v as LocalPortraitSelection);
 const normalize=(value:unknown):unknown=>Array.isArray(value)?value.map(normalize):value&&typeof value==='object'?Object.fromEntries(Object.entries(value).sort(([a],[b])=>a.localeCompare(b)).map(([key,item])=>[key,normalize(item)])):value;
 if(JSON.stringify(normalize(prepared))!==JSON.stringify(normalize(freezeLocalPortraitSelection(selected))))throw new Error('LOCAL_PORTRAIT_PREPARATION_MISMATCH');
 return prepared;
}
export function verifyLocalPortraitRecoverySource(v:LocalPortraitSelection,live:V2VersionBinding):LocalPortraitSelection{
 const frozen=freezeLocalPortraitSelection(v),base=frozen.params.base;
 if(!binding(live)||base.asset_version_id!==live.asset_version_id||base.source.artifact_id!==live.source.artifact_id||base.source.sha256!==live.source.sha256||base.canvas_hash!==live.canvas_hash)throw new Error('LOCAL_PORTRAIT_RECOVERY_SOURCE_CHANGED');
 return frozen;
}
export function readLocalPortraitTicket(v:unknown):LocalPortraitTicket{
 if(!fields(v,['run_id','workflow_id','task_id','state'])||!uuid(v.run_id)||!uuid(v.workflow_id)||!uuid(v.task_id)||v.state!=='queued')throw new Error('LOCAL_PORTRAIT_TICKET_INVALID');
 return {run_id:v.run_id,workflow_id:v.workflow_id,task_id:v.task_id,state:'queued'};
}
