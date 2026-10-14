import type {V2VersionBinding} from './catalog-types';
import {freezeRelightCandidateQuery} from './relight-candidates';
export type RelightAnalysisOperation='depth.predict'|'mask.person';
export type RelightAnalysisIntent={schema_version:1;snapshot_id:string;base:V2VersionBinding;operation:RelightAnalysisOperation};
/** Freeze public intent before async transport. Deployment pins never enter it. */
export function freezeRelightAnalysisIntent(value:RelightAnalysisIntent):RelightAnalysisIntent{
 if(!value||typeof value!=='object'||Array.isArray(value)||Object.keys(value).sort().join(',')!=='base,operation,schema_version,snapshot_id'||value.schema_version!==1||!['depth.predict','mask.person'].includes(value.operation))throw new Error('RELIGHT_ANALYSIS_INTENT_INVALID');
 freezeRelightCandidateQuery({snapshot_id:value.snapshot_id,base:value.base,kind:'depth',after:null});
 return structuredClone(value);
}
