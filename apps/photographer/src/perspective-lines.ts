import type {ArtifactRef,V2VersionBinding} from './catalog-types';
import {freezePerspectiveIntent,type PerspectivePoint,type PerspectiveIntent} from './perspective-intent';
export type PerspectiveLine={a:PerspectivePoint;b:PerspectivePoint;family:'horizontal'|'vertical'};
export type PerspectiveLinesIntent={schema_version:1;snapshot_id:string;base:V2VersionBinding;line_segments:{schema_version:1;lines:PerspectiveLine[]}};
export type PerspectiveLinesReceipt={schema_version:1;tenant_id:string;project_id:string;snapshot_id:string;base:V2VersionBinding;lines:ArtifactRef};
// The initial guides are only editing handles. A new publication needs an
// explicit user edit or an adopted detector result; retries retain their body.
export function canPublishPerspectiveLines(authored:boolean,retrying:boolean):boolean{
 return authored||retrying;
}
const fields=(v:unknown,keys:string[]):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.prototype.hasOwnProperty.call(v,k));
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const unit=(v:unknown):v is number=>typeof v==='number'&&Number.isFinite(v)&&v>=0&&v<=1;
export function freezePerspectiveLinesIntent(value:PerspectiveLinesIntent):PerspectiveLinesIntent{
 const invalid=()=>{throw new Error('PERSPECTIVE_LINES_INVALID');};
 if(!fields(value,['schema_version','snapshot_id','base','line_segments'])||value.schema_version!==1)invalid();
 freezePerspectiveIntent({schema_version:1,snapshot_id:value.snapshot_id,params:{schema_version:1,base:value.base,strength:0,mode:'manual',corners:[{x:0,y:0},{x:1,y:0},{x:1,y:1},{x:0,y:1}],lines:null,max_crop_fraction:1}});
 const document=value.line_segments;
 if(!fields(document,['schema_version','lines'])||document.schema_version!==1||!Array.isArray(document.lines)||document.lines.length<4||document.lines.length>1000)invalid();
 const counts={horizontal:0,vertical:0};
 for(const line of document.lines){
  if(!fields(line,['a','b','family'])||(line.family!=='horizontal'&&line.family!=='vertical'))invalid();
  for(const point of [line.a,line.b])if(!fields(point,['x','y'])||!unit(point.x)||!unit(point.y))invalid();
  if(line.a.x===line.b.x&&line.a.y===line.b.y)invalid();
  counts[line.family]++;
 }
 if(counts.horizontal<2||counts.vertical<2)invalid();
 return structuredClone(value);
}
export function readPerspectiveLinesReceipt(value:unknown,tenant:string,project:string,request:PerspectiveLinesIntent):PerspectiveLinesReceipt{
 const original=freezePerspectiveLinesIntent(request);
 if(!fields(value,['schema_version','tenant_id','project_id','snapshot_id','base','lines'])||value.schema_version!==1||!uuid(tenant)||!uuid(project)||value.tenant_id!==tenant||value.project_id!==project||value.snapshot_id!==original.snapshot_id||!fields(value.lines,['artifact_id','sha256']))throw new Error('PERSPECTIVE_LINES_RECEIPT_INVALID');
 const candidate=freezePerspectiveIntent({schema_version:1,snapshot_id:value.snapshot_id as string,params:{schema_version:1,base:value.base as V2VersionBinding,strength:1,mode:'auto',corners:null,lines:value.lines as ArtifactRef,max_crop_fraction:1}});
 const base=candidate.params.base;
 if(base.asset_version_id!==original.base.asset_version_id||base.canvas_hash!==original.base.canvas_hash||base.source.artifact_id!==original.base.source.artifact_id||base.source.sha256!==original.base.source.sha256||candidate.params.lines!.artifact_id===base.source.artifact_id)throw new Error('PERSPECTIVE_LINES_RECEIPT_INVALID');
 return structuredClone(value) as PerspectiveLinesReceipt;
}
export function perspectiveAutoIntent(receipt:PerspectiveLinesReceipt,strength:number,maxCropFraction:number):PerspectiveIntent{
 return freezePerspectiveIntent({schema_version:1,snapshot_id:receipt.snapshot_id,params:{schema_version:1,base:receipt.base,strength,mode:'auto',corners:null,lines:receipt.lines,max_crop_fraction:maxCropFraction}});
}
