import type {ArtifactRef,V2VersionBinding} from './catalog-types';

export type ReflectionPreparation={schema_version:1;snapshot_id:string;ordinal:number;base:V2VersionBinding;strength:number;mask:ArtifactRef;protected:ArtifactRef;target_pixel:ArtifactRef;protected_pixel:ArtifactRef};
export type ReflectionParams={schema_version:1;base:V2VersionBinding;strength:number;mask:ArtifactRef;protected:ArtifactRef;model_lock_sha:string;calibration:ArtifactRef};
export type ReflectionIntent={schema_version:1;snapshot_id:string;ordinal:number;params_proposal:ReflectionParams;target_pixel:ArtifactRef;protected_pixel:ArtifactRef};
export type ReflectionTicket={run_id:string;workflow_id:string;task_id:string;state:'queued'};

const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const sha=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
const fields=(v:unknown,keys:string[]):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.prototype.hasOwnProperty.call(v,k));
const ref=(v:unknown):v is ArtifactRef=>fields(v,['artifact_id','sha256'])&&uuid(v.artifact_id)&&sha(v.sha256);
const binding=(v:unknown):v is V2VersionBinding=>fields(v,['asset_version_id','source','canvas_hash'])&&uuid(v.asset_version_id)&&ref(v.source)&&sha(v.canvas_hash);
const distinct=(refs:ArtifactRef[])=>new Set(refs.map(v=>v.artifact_id)).size===refs.length;

export function freezeReflectionPreparation(v:ReflectionPreparation):ReflectionPreparation{
 if(!fields(v,['schema_version','snapshot_id','ordinal','base','strength','mask','protected','target_pixel','protected_pixel'])||v.schema_version!==1||!uuid(v.snapshot_id)||!Number.isInteger(v.ordinal)||v.ordinal<0||v.ordinal>=5000||!binding(v.base)||typeof v.strength!=='number'||!Number.isFinite(v.strength)||v.strength<0||v.strength>1||![v.mask,v.protected,v.target_pixel,v.protected_pixel].every(ref)||!distinct([v.base.source,v.mask,v.protected,v.target_pixel,v.protected_pixel]))throw new Error('REFLECTION_PREPARATION_INVALID');
 return structuredClone(v);
}

export function freezeReflectionIntent(v:ReflectionIntent):ReflectionIntent{
 if(!fields(v,['schema_version','snapshot_id','ordinal','params_proposal','target_pixel','protected_pixel'])||v.schema_version!==1||!uuid(v.snapshot_id)||!Number.isInteger(v.ordinal)||v.ordinal<0||v.ordinal>=5000||!ref(v.target_pixel)||!ref(v.protected_pixel))throw new Error('REFLECTION_INTENT_INVALID');
 const p=v.params_proposal;
 if(!fields(p,['schema_version','base','strength','mask','protected','model_lock_sha','calibration'])||p.schema_version!==1||!binding(p.base)||typeof p.strength!=='number'||!Number.isFinite(p.strength)||p.strength<0||p.strength>1||!sha(p.model_lock_sha)||![p.mask,p.protected,p.calibration].every(ref)||!distinct([p.base.source,p.mask,p.protected,v.target_pixel,v.protected_pixel,p.calibration]))throw new Error('REFLECTION_INTENT_INVALID');
 return structuredClone(v);
}

export function readPreparedReflectionIntent(v:unknown,selected:ReflectionPreparation):ReflectionIntent{
 const result=freezeReflectionIntent(v as ReflectionIntent),p=result.params_proposal;
 const same=(a:ArtifactRef,b:ArtifactRef)=>a.artifact_id===b.artifact_id&&a.sha256===b.sha256;
 if(result.snapshot_id!==selected.snapshot_id||result.ordinal!==selected.ordinal||p.strength!==selected.strength||p.base.asset_version_id!==selected.base.asset_version_id||p.base.canvas_hash!==selected.base.canvas_hash||!same(p.base.source,selected.base.source)||!same(p.mask,selected.mask)||!same(p.protected,selected.protected)||!same(result.target_pixel,selected.target_pixel)||!same(result.protected_pixel,selected.protected_pixel))throw new Error('REFLECTION_PREPARATION_MISMATCH');
 return result;
}

export function verifyReflectionRecoverySource(v:ReflectionIntent,live:V2VersionBinding):ReflectionIntent{
 const frozen=freezeReflectionIntent(v),base=frozen.params_proposal.base;
 if(!binding(live)||base.asset_version_id!==live.asset_version_id||base.source.artifact_id!==live.source.artifact_id||base.source.sha256!==live.source.sha256||base.canvas_hash!==live.canvas_hash)throw new Error('REFLECTION_RECOVERY_SOURCE_CHANGED');
 return frozen;
}

export function readReflectionTicket(v:unknown):ReflectionTicket{
 if(!fields(v,['run_id','workflow_id','task_id','state'])||!uuid(v.run_id)||!uuid(v.workflow_id)||!uuid(v.task_id)||v.state!=='queued')throw new Error('REFLECTION_TICKET_INVALID');
 return {run_id:v.run_id,workflow_id:v.workflow_id,task_id:v.task_id,state:'queued'};
}
