import type {ArtifactRef,V2VersionBinding} from './catalog-types';

export type OpenEyesSelection={schema_version:1;shoot_session_id:string;snapshot_id:string;ordinal:number;base:V2VersionBinding;donor:V2VersionBinding;strength:number;eyes_mask:ArtifactRef;protected:ArtifactRef;target_pixel:ArtifactRef;protected_pixel:ArtifactRef;source_faces:ArtifactRef;donor_faces:ArtifactRef;source_observation_id:string;donor_observation_id:string};
export type OpenEyesParams={schema_version:1;base:V2VersionBinding;strength:number;donor:V2VersionBinding;source_face_id:string;donor_face_id:string;eyes_mask:ArtifactRef;protected:ArtifactRef;confirmation:ArtifactRef;source_faces:ArtifactRef;donor_faces:ArtifactRef};
export type OpenEyesIntent={schema_version:1;shoot_session_id:string;snapshot_id:string;ordinal:number;params_proposal:OpenEyesParams;target_pixel:ArtifactRef;protected_pixel:ArtifactRef;source_observation_id:string;donor_observation_id:string};
export type OpenEyesTicket={run_id:string;workflow_id:string;task_id:string;state:'queued'};

const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const sha=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
const fields=(v:unknown,keys:string[]):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.prototype.hasOwnProperty.call(v,k));
const ref=(v:unknown):v is ArtifactRef=>fields(v,['artifact_id','sha256'])&&uuid(v.artifact_id)&&sha(v.sha256);
const binding=(v:unknown):v is V2VersionBinding=>fields(v,['asset_version_id','source','canvas_hash'])&&uuid(v.asset_version_id)&&ref(v.source)&&sha(v.canvas_hash);
const sameRef=(a:ArtifactRef,b:ArtifactRef)=>a.artifact_id===b.artifact_id&&a.sha256===b.sha256;
const sameBinding=(a:V2VersionBinding,b:V2VersionBinding)=>a.asset_version_id===b.asset_version_id&&a.canvas_hash===b.canvas_hash&&sameRef(a.source,b.source);

export function freezeOpenEyesSelection(v:OpenEyesSelection):OpenEyesSelection{
 if(!fields(v,['schema_version','shoot_session_id','snapshot_id','ordinal','base','donor','strength','eyes_mask','protected','target_pixel','protected_pixel','source_faces','donor_faces','source_observation_id','donor_observation_id'])||v.schema_version!==1||!uuid(v.shoot_session_id)||!uuid(v.snapshot_id)||!Number.isInteger(v.ordinal)||v.ordinal<0||v.ordinal>=5000||!binding(v.base)||!binding(v.donor)||v.base.asset_version_id===v.donor.asset_version_id||typeof v.strength!=='number'||!Number.isFinite(v.strength)||v.strength<0||v.strength>1||![v.eyes_mask,v.protected,v.target_pixel,v.protected_pixel,v.source_faces,v.donor_faces].every(ref)||!uuid(v.source_observation_id)||!uuid(v.donor_observation_id)||v.source_observation_id===v.donor_observation_id)throw new Error('OPEN_EYES_SELECTION_INVALID');
 const ids=[v.base.source.artifact_id,v.donor.source.artifact_id,v.eyes_mask.artifact_id,v.protected.artifact_id,v.target_pixel.artifact_id,v.protected_pixel.artifact_id,v.source_faces.artifact_id,v.donor_faces.artifact_id];
 if(new Set(ids).size!==ids.length)throw new Error('OPEN_EYES_SELECTION_INVALID');
 return structuredClone(v);
}

export function freezeOpenEyesIntent(v:OpenEyesIntent):OpenEyesIntent{
 if(!fields(v,['schema_version','shoot_session_id','snapshot_id','ordinal','params_proposal','target_pixel','protected_pixel','source_observation_id','donor_observation_id'])||v.schema_version!==1||!uuid(v.shoot_session_id)||!uuid(v.snapshot_id)||!Number.isInteger(v.ordinal)||v.ordinal<0||v.ordinal>=5000||!uuid(v.source_observation_id)||!uuid(v.donor_observation_id)||v.source_observation_id===v.donor_observation_id||!ref(v.target_pixel)||!ref(v.protected_pixel))throw new Error('OPEN_EYES_INTENT_INVALID');
 const p=v.params_proposal;
 if(!fields(p,['schema_version','base','strength','donor','source_face_id','donor_face_id','eyes_mask','protected','confirmation','source_faces','donor_faces'])||p.schema_version!==1||!binding(p.base)||!binding(p.donor)||p.base.asset_version_id===p.donor.asset_version_id||typeof p.strength!=='number'||!Number.isFinite(p.strength)||p.strength<0||p.strength>1||!uuid(p.source_face_id)||!uuid(p.donor_face_id)||![p.eyes_mask,p.protected,p.confirmation,p.source_faces,p.donor_faces].every(ref))throw new Error('OPEN_EYES_INTENT_INVALID');
 const selected:OpenEyesSelection={schema_version:1,shoot_session_id:v.shoot_session_id,snapshot_id:v.snapshot_id,ordinal:v.ordinal,base:p.base,donor:p.donor,strength:p.strength,eyes_mask:p.eyes_mask,protected:p.protected,target_pixel:v.target_pixel,protected_pixel:v.protected_pixel,source_faces:p.source_faces,donor_faces:p.donor_faces,source_observation_id:v.source_observation_id,donor_observation_id:v.donor_observation_id};
 freezeOpenEyesSelection(selected);
 if([p.base.source,p.donor.source,p.eyes_mask,p.protected,p.source_faces,p.donor_faces,v.target_pixel,v.protected_pixel].some(r=>r.artifact_id===p.confirmation.artifact_id))throw new Error('OPEN_EYES_INTENT_INVALID');
 return structuredClone(v);
}

export function readPreparedOpenEyesIntent(v:unknown,selected:OpenEyesSelection):OpenEyesIntent{
 const intent=freezeOpenEyesIntent(v as OpenEyesIntent),p=intent.params_proposal;
 if(intent.shoot_session_id!==selected.shoot_session_id||intent.snapshot_id!==selected.snapshot_id||intent.ordinal!==selected.ordinal||intent.source_observation_id!==selected.source_observation_id||intent.donor_observation_id!==selected.donor_observation_id||p.strength!==selected.strength||!sameBinding(p.base,selected.base)||!sameBinding(p.donor,selected.donor)||!sameRef(p.eyes_mask,selected.eyes_mask)||!sameRef(p.protected,selected.protected)||!sameRef(p.source_faces,selected.source_faces)||!sameRef(p.donor_faces,selected.donor_faces)||!sameRef(intent.target_pixel,selected.target_pixel)||!sameRef(intent.protected_pixel,selected.protected_pixel))throw new Error('OPEN_EYES_PREPARATION_MISMATCH');
 return intent;
}

export function readOpenEyesTicket(v:unknown):OpenEyesTicket{
 if(!fields(v,['run_id','workflow_id','task_id','state'])||!uuid(v.run_id)||!uuid(v.workflow_id)||!uuid(v.task_id)||v.state!=='queued')throw new Error('OPEN_EYES_TICKET_INVALID');
 return {run_id:v.run_id,workflow_id:v.workflow_id,task_id:v.task_id,state:'queued'};
}
