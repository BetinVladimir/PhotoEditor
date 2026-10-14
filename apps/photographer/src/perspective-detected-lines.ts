import type {ArtifactRef,V2VersionBinding} from './catalog-types';
import {perspectiveProposalScope} from './perspective-proposal';
import {freezePerspectiveLinesIntent,type PerspectiveLinesIntent,type PerspectiveLine} from './perspective-lines';
export type PerspectiveDetectedLines={schema_version:1;tenant_id:string;project_id:string;producer_task_id:string;result_id:string;result:ArtifactRef;detection:{schema_version:1;base:V2VersionBinding;status:'available'|'unsupported';lines:{schema_version:1;lines:PerspectiveLine[]}|null}};
const fields=(v:unknown,keys:string[]):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.prototype.hasOwnProperty.call(v,k));
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
export function readPerspectiveDetectedLines(value:unknown,tenant:string,project:string,producer:string,base:V2VersionBinding):PerspectiveDetectedLines {
 const scope=perspectiveProposalScope(tenant,project,producer,base);
 const invalid=():never=>{throw new Error('PERSPECTIVE_DETECTION_INVALID');};
 if(!fields(value,['schema_version','tenant_id','project_id','producer_task_id','result_id','result','detection'])||value.schema_version!==1||value.tenant_id!==scope.tenant||value.project_id!==scope.project||value.producer_task_id!==scope.producer||!uuid(value.result_id))invalid();
 const v=value as Record<string,unknown>;
 if(!fields(v.result,['artifact_id','sha256'])||!uuid(v.result.artifact_id)||typeof v.result.sha256!=='string'||!/^[0-9a-f]{64}$/.test(v.result.sha256)||v.result.artifact_id===scope.base.source.artifact_id)invalid();
 if(!fields(v.detection,['schema_version','base','status','lines'])||v.detection.schema_version!==1)invalid();
 const d=v.detection as Record<string,unknown>;
 const candidate=perspectiveProposalScope(tenant,project,producer,d.base as V2VersionBinding).base;
 if(candidate.asset_version_id!==scope.base.asset_version_id||candidate.canvas_hash!==scope.base.canvas_hash||candidate.source.artifact_id!==scope.base.source.artifact_id||candidate.source.sha256!==scope.base.source.sha256)invalid();
 if(d.status==='unsupported'){if(d.lines!==null)invalid();}
 else if(d.status==='available'){
  if(!fields(d.lines,['schema_version','lines'])||!Array.isArray(d.lines.lines)||d.lines.lines.length>128)invalid();
  const lines=freezePerspectiveLinesIntent({schema_version:1,snapshot_id:producer,base:candidate,line_segments:d.lines as PerspectiveLinesIntent['line_segments']}).line_segments.lines;
  for(const family of ['horizontal','vertical'] as const){const centers=lines.filter(l=>l.family===family).map(l=>family==='horizontal'?(l.a.y+l.b.y)/2:(l.a.x+l.b.x)/2);if(Math.max(...centers)-Math.min(...centers)<.1)invalid();}
 }else invalid();
 return structuredClone(value) as PerspectiveDetectedLines;
}
// Produces an editable publication intent, never a correction Apply decision.
export function detectedPerspectiveLinesIntent(value:PerspectiveDetectedLines,snapshot:string):PerspectiveLinesIntent {
 const owned=readPerspectiveDetectedLines(value,value.tenant_id,value.project_id,value.producer_task_id,value.detection.base);
 if(owned.detection.status!=='available'||owned.detection.lines===null)throw new Error('PERSPECTIVE_DETECTION_UNSUPPORTED');
 return freezePerspectiveLinesIntent({schema_version:1,snapshot_id:snapshot,base:owned.detection.base,line_segments:owned.detection.lines});
}
