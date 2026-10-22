import type {ArtifactRef,V2VersionBinding} from './catalog-types';

export type GlareDonorParams={schema_version:1;mode:'donor';base:V2VersionBinding;donor:V2VersionBinding;strength:number;glasses_mask:ArtifactRef;protected:ArtifactRef;confirmation:ArtifactRef;source_faces:ArtifactRef;donor_faces:ArtifactRef;model_lock_sha:string;calibration:ArtifactRef};
export type GlareDonorIntent={schema_version:1;shoot_session_id:string;snapshot_id:string;ordinal:number;params_proposal:GlareDonorParams;source_observation_id:string;donor_observation_id:string};
export type GlareDonorPreparation={schema_version:1;shoot_session_id:string;snapshot_id:string;ordinal:number;base:V2VersionBinding;donor:V2VersionBinding;strength:number;glasses_mask:ArtifactRef;protected:ArtifactRef;target_pixel:ArtifactRef;protected_pixel:ArtifactRef;source_faces:ArtifactRef;donor_faces:ArtifactRef;source_observation_id:string;donor_observation_id:string};
export type GlareDonorTicket={run_id:string;workflow_id:string;task_id:string;state:'queued'};

const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const sha=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
const fields=(v:unknown,keys:string[]):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.prototype.hasOwnProperty.call(v,k));
const ref=(v:unknown):v is ArtifactRef=>fields(v,['artifact_id','sha256'])&&uuid(v.artifact_id)&&sha(v.sha256);
const binding=(v:unknown):v is V2VersionBinding=>fields(v,['asset_version_id','source','canvas_hash'])&&uuid(v.asset_version_id)&&ref(v.source)&&sha(v.canvas_hash);

export function freezeGlareDonorPreparation(v:GlareDonorPreparation):GlareDonorPreparation{
 if(!fields(v,['schema_version','shoot_session_id','snapshot_id','ordinal','base','donor','strength','glasses_mask','protected','target_pixel','protected_pixel','source_faces','donor_faces','source_observation_id','donor_observation_id'])||v.schema_version!==1||!uuid(v.shoot_session_id)||!uuid(v.snapshot_id)||!Number.isInteger(v.ordinal)||v.ordinal<0||v.ordinal>=5000||!binding(v.base)||!binding(v.donor)||v.base.asset_version_id===v.donor.asset_version_id||typeof v.strength!=='number'||!Number.isFinite(v.strength)||v.strength<0||v.strength>1||!uuid(v.source_observation_id)||!uuid(v.donor_observation_id)||v.source_observation_id===v.donor_observation_id||![v.glasses_mask,v.protected,v.target_pixel,v.protected_pixel,v.source_faces,v.donor_faces].every(ref))throw new Error('GLARE_DONOR_PREPARATION_INVALID');
 const ids=[v.base.source.artifact_id,v.donor.source.artifact_id,v.glasses_mask.artifact_id,v.protected.artifact_id,v.target_pixel.artifact_id,v.protected_pixel.artifact_id,v.source_faces.artifact_id,v.donor_faces.artifact_id];
 if(new Set(ids).size!==ids.length)throw new Error('GLARE_DONOR_PREPARATION_INVALID');
 return structuredClone(v);
}

export function readPreparedGlareDonorIntent(v:unknown,selected:GlareDonorPreparation):GlareDonorIntent{
 const result=freezeGlareDonorIntent(v as GlareDonorIntent),p=result.params_proposal;
 if(result.shoot_session_id!==selected.shoot_session_id||result.snapshot_id!==selected.snapshot_id||result.ordinal!==selected.ordinal||result.source_observation_id!==selected.source_observation_id||result.donor_observation_id!==selected.donor_observation_id||p.strength!==selected.strength||
  p.base.asset_version_id!==selected.base.asset_version_id||p.base.source.artifact_id!==selected.base.source.artifact_id||p.base.source.sha256!==selected.base.source.sha256||p.base.canvas_hash!==selected.base.canvas_hash||
  p.donor.asset_version_id!==selected.donor.asset_version_id||p.donor.source.artifact_id!==selected.donor.source.artifact_id||p.donor.source.sha256!==selected.donor.source.sha256||p.donor.canvas_hash!==selected.donor.canvas_hash||
  ![p.glasses_mask,p.protected,p.source_faces,p.donor_faces].every((r,i)=>r.artifact_id===[selected.glasses_mask,selected.protected,selected.source_faces,selected.donor_faces][i].artifact_id&&r.sha256===[selected.glasses_mask,selected.protected,selected.source_faces,selected.donor_faces][i].sha256))throw new Error('GLARE_DONOR_PREPARATION_MISMATCH');
 return result;
}

/** Freeze only authored selection. Qualification and capacity never cross this boundary. */
export function freezeGlareDonorIntent(v:GlareDonorIntent):GlareDonorIntent{
 if(!fields(v,['schema_version','shoot_session_id','snapshot_id','ordinal','params_proposal','source_observation_id','donor_observation_id'])||v.schema_version!==1||!uuid(v.shoot_session_id)||!uuid(v.snapshot_id)||!Number.isInteger(v.ordinal)||v.ordinal<0||v.ordinal>=5000||!uuid(v.source_observation_id)||!uuid(v.donor_observation_id)||v.source_observation_id===v.donor_observation_id)throw new Error('GLARE_DONOR_INTENT_INVALID');
 const p=v.params_proposal;
 if(!fields(p,['schema_version','mode','base','donor','strength','glasses_mask','protected','confirmation','source_faces','donor_faces','model_lock_sha','calibration'])||p.schema_version!==1||p.mode!=='donor'||!binding(p.base)||!binding(p.donor)||p.base.asset_version_id===p.donor.asset_version_id||typeof p.strength!=='number'||!Number.isFinite(p.strength)||p.strength<0||p.strength>1||!sha(p.model_lock_sha)||![p.glasses_mask,p.protected,p.confirmation,p.source_faces,p.donor_faces,p.calibration].every(ref))throw new Error('GLARE_DONOR_INTENT_INVALID');
 const IDs=[p.base.source.artifact_id,p.donor.source.artifact_id,p.glasses_mask.artifact_id,p.protected.artifact_id,p.confirmation.artifact_id,p.source_faces.artifact_id,p.donor_faces.artifact_id,p.calibration.artifact_id];
 if(new Set(IDs).size!==IDs.length)throw new Error('GLARE_DONOR_INTENT_INVALID');
 return structuredClone(v);
}

export function readGlareDonorTicket(v:unknown):GlareDonorTicket{
 if(!fields(v,['run_id','workflow_id','task_id','state'])||!uuid(v.run_id)||!uuid(v.workflow_id)||!uuid(v.task_id)||v.state!=='queued')throw new Error('GLARE_DONOR_TICKET_INVALID');
 return {run_id:v.run_id,workflow_id:v.workflow_id,task_id:v.task_id,state:'queued'};
}

export function verifyGlareDonorRecoverySource(v:GlareDonorIntent,live:V2VersionBinding):GlareDonorIntent{
 const frozen=freezeGlareDonorIntent(v),base=frozen.params_proposal.base;
 if(!binding(live)||base.asset_version_id!==live.asset_version_id||base.canvas_hash!==live.canvas_hash||base.source.artifact_id!==live.source.artifact_id||base.source.sha256!==live.source.sha256)throw new Error('GLARE_DONOR_RECOVERY_SOURCE_CHANGED');
 return frozen;
}
