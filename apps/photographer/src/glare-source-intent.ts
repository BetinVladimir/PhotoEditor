import type {ArtifactRef,V2VersionBinding} from './catalog-types';
import {readGlareDonorTicket,type GlareDonorTicket} from './glare-donor-intent';
export type GlareSourcePreparation={schema_version:1;shoot_session_id:string;snapshot_id:string;ordinal:number;base:V2VersionBinding;strength:number;glasses_mask:ArtifactRef;protected:ArtifactRef;target_pixel:ArtifactRef;protected_pixel:ArtifactRef;source_faces:ArtifactRef;source_observation_id:string};
export type GlareSourceParams={schema_version:1;mode:'generative';base:V2VersionBinding;strength:number;donor:null;donor_faces:null;glasses_mask:ArtifactRef;protected:ArtifactRef;confirmation:ArtifactRef;source_faces:ArtifactRef;model_lock_sha:string;calibration:ArtifactRef};
export type GlareSourceIntent={schema_version:1;shoot_session_id:string;snapshot_id:string;ordinal:number;params_proposal:GlareSourceParams;source_observation_id:string};
export type GlareSourceTicket=GlareDonorTicket;
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const sha=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
const fields=(v:unknown,keys:string[]):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.prototype.hasOwnProperty.call(v,k));
const ref=(v:unknown):v is ArtifactRef=>fields(v,['artifact_id','sha256'])&&uuid(v.artifact_id)&&sha(v.sha256);
const binding=(v:unknown):v is V2VersionBinding=>fields(v,['asset_version_id','source','canvas_hash'])&&uuid(v.asset_version_id)&&ref(v.source)&&sha(v.canvas_hash);
const unit=(v:unknown)=>typeof v==='number'&&Number.isFinite(v)&&v>=0&&v<=1;
const distinct=(refs:ArtifactRef[])=>new Set(refs.map(r=>r.artifact_id)).size===refs.length;
export function freezeGlareSourcePreparation(v:GlareSourcePreparation):GlareSourcePreparation{
 if(!fields(v,['schema_version','shoot_session_id','snapshot_id','ordinal','base','strength','glasses_mask','protected','target_pixel','protected_pixel','source_faces','source_observation_id'])||v.schema_version!==1||!uuid(v.shoot_session_id)||!uuid(v.snapshot_id)||!Number.isInteger(v.ordinal)||v.ordinal<0||v.ordinal>=5000||!binding(v.base)||!unit(v.strength)||!uuid(v.source_observation_id)||![v.glasses_mask,v.protected,v.target_pixel,v.protected_pixel,v.source_faces].every(ref)||!distinct([v.base.source,v.glasses_mask,v.protected,v.target_pixel,v.protected_pixel,v.source_faces]))throw new Error('GLARE_SOURCE_PREPARATION_INVALID');return structuredClone(v);
}
export function freezeGlareSourceIntent(v:GlareSourceIntent):GlareSourceIntent{
 if(!fields(v,['schema_version','shoot_session_id','snapshot_id','ordinal','params_proposal','source_observation_id'])||v.schema_version!==1||!uuid(v.shoot_session_id)||!uuid(v.snapshot_id)||!Number.isInteger(v.ordinal)||v.ordinal<0||v.ordinal>=5000||!uuid(v.source_observation_id))throw new Error('GLARE_SOURCE_INTENT_INVALID');
 const p=v.params_proposal;
 if(!fields(p,['schema_version','mode','base','strength','donor','donor_faces','glasses_mask','protected','confirmation','source_faces','model_lock_sha','calibration'])||p.schema_version!==1||p.mode!=='generative'||p.donor!==null||p.donor_faces!==null||!binding(p.base)||!unit(p.strength)||!sha(p.model_lock_sha)||![p.glasses_mask,p.protected,p.confirmation,p.source_faces,p.calibration].every(ref)||!distinct([p.base.source,p.glasses_mask,p.protected,p.confirmation,p.source_faces,p.calibration]))throw new Error('GLARE_SOURCE_INTENT_INVALID');return structuredClone(v);
}
export function readPreparedGlareSourceIntent(v:unknown,selection:GlareSourcePreparation):GlareSourceIntent{
 const selected=freezeGlareSourcePreparation(selection),result=freezeGlareSourceIntent(v as GlareSourceIntent),p=result.params_proposal;
 if(result.shoot_session_id!==selected.shoot_session_id||result.snapshot_id!==selected.snapshot_id||result.ordinal!==selected.ordinal||result.source_observation_id!==selected.source_observation_id||p.strength!==selected.strength||p.base.asset_version_id!==selected.base.asset_version_id||p.base.canvas_hash!==selected.base.canvas_hash||p.base.source.artifact_id!==selected.base.source.artifact_id||p.base.source.sha256!==selected.base.source.sha256||![p.glasses_mask,p.protected,p.source_faces].every((r,i)=>r.artifact_id===[selected.glasses_mask,selected.protected,selected.source_faces][i].artifact_id&&r.sha256===[selected.glasses_mask,selected.protected,selected.source_faces][i].sha256))throw new Error('GLARE_SOURCE_PREPARATION_MISMATCH');return result;
}
export function verifyGlareSourceRecovery(v:GlareSourceIntent,live:V2VersionBinding){const frozen=freezeGlareSourceIntent(v),base=frozen.params_proposal.base;if(!binding(live)||base.asset_version_id!==live.asset_version_id||base.canvas_hash!==live.canvas_hash||base.source.artifact_id!==live.source.artifact_id||base.source.sha256!==live.source.sha256)throw new Error('GLARE_SOURCE_RECOVERY_CHANGED');return frozen;}
export const readGlareSourceTicket=readGlareDonorTicket;
