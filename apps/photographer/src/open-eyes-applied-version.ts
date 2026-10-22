import type {NegativeResultSelection} from './negative-result-selection';
import type {ArtifactRef} from './catalog-types';
export type OpenEyesAppliedVersion={asset_id:string;project_id:string;asset_version_id:string;review_id:string;decision_hash:string;source:ArtifactRef};
export function readOpenEyesAppliedVersion(value:unknown,selection:NegativeResultSelection):OpenEyesAppliedVersion{
 const fail=():never=>{throw new Error('OPEN_EYES_APPLY_RESULT_INVALID')};
 const uuid=(v:unknown)=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
 if(!value||typeof value!=='object'||Array.isArray(value))return fail();
 const v=value as OpenEyesAppliedVersion;
 const keys=['asset_id','project_id','asset_version_id','review_id','decision_hash','source'];
 if(Object.keys(v).length!==keys.length||!keys.every(k=>Object.hasOwn(v,k))||!uuid(v.asset_id)||!uuid(v.project_id)||!uuid(v.asset_version_id)||
  v.review_id!==selection.review_id||v.decision_hash!==selection.decision_hash||!selection.image||!['candidate','base'].includes(selection.route)||
  !v.source||typeof v.source!=='object'||Array.isArray(v.source)||Object.keys(v.source).length!==2||
  v.source.artifact_id!==selection.image.artifact_id||v.source.sha256!==selection.image.sha256)return fail();
 return structuredClone(v);
}
