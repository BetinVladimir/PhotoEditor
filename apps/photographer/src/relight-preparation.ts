import {freezeRelightIntent,type RelightIntent} from './relight-intent';
import type {ArtifactRef,V2VersionBinding} from './catalog-types';
export type RelightPreparationRequest={schema_version:1;snapshot_id:string;base:V2VersionBinding;depth:ArtifactRef;subject:ArtifactRef;protected:ArtifactRef;subject_kind:'person'|'manual';ordinal:number};
export function freezeRelightPreparation(v:RelightPreparationRequest):RelightPreparationRequest{
 if(!v||Object.keys(v).sort().join(',')!=='base,depth,ordinal,protected,schema_version,snapshot_id,subject,subject_kind'||v.schema_version!==1)throw new Error('RELIGHT_PREPARATION_INVALID');
 // Placeholder is only a local shape witness; calibration is selected by server.
 freezeRelightIntent({schema_version:1,snapshot_id:v.snapshot_id,ordinal:v.ordinal,subject_kind:v.subject_kind,params:{schema_version:1,base:v.base,depth:v.depth,subject:v.subject,protected:v.protected,calibration:v.depth,strength:.5,lights:[{x:.5,y:.5,radius:.3,ev:0}]}});
 return structuredClone(v);
}
export function readPreparedRelight(value:unknown,request:RelightPreparationRequest):RelightIntent{
 const intent=freezeRelightIntent(value as RelightIntent);
 if(intent.snapshot_id!==request.snapshot_id||intent.ordinal!==request.ordinal||intent.subject_kind!==request.subject_kind)throw new Error('RELIGHT_PREPARATION_SCOPE_CHANGED');
 const sameRef=(a:ArtifactRef,b:ArtifactRef)=>a.artifact_id===b.artifact_id&&a.sha256===b.sha256;
 if(intent.params.base.asset_version_id!==request.base.asset_version_id||intent.params.base.canvas_hash!==request.base.canvas_hash||!sameRef(intent.params.base.source,request.base.source)||!sameRef(intent.params.depth,request.depth)||!sameRef(intent.params.subject,request.subject)||!sameRef(intent.params.protected,request.protected))throw new Error('RELIGHT_PREPARATION_SCOPE_CHANGED');
 return intent;
}
