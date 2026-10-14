import type {ArtifactRef,V2VersionBinding} from './catalog-types';
export type PerspectiveProposalCanvas={width:number;height:number;orientation:number;space_id:string;source_sha256:string};
export type PerspectiveProposal={schema_version:1;tenant_id:string;project_id:string;producer_task_id:string;result_id:string;result:ArtifactRef;base:V2VersionBinding;working_image:ArtifactRef;changed_mask:ArtifactRef;report:ArtifactRef;canvas:PerspectiveProposalCanvas;status:'pass'|'review';requires_review:boolean};
const fields=(v:unknown,keys:string[]):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.prototype.hasOwnProperty.call(v,k));
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const sha=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
const ref=(v:unknown):v is ArtifactRef=>fields(v,['artifact_id','sha256'])&&uuid(v.artifact_id)&&sha(v.sha256);
const base=(v:unknown):v is V2VersionBinding=>fields(v,['asset_version_id','source','canvas_hash'])&&uuid(v.asset_version_id)&&ref(v.source)&&sha(v.canvas_hash);
export function perspectiveProposalScope(tenant:string,project:string,producer:string,expected:V2VersionBinding){
 if(!uuid(tenant)||!uuid(project)||!uuid(producer)||!base(expected))throw new Error('PERSPECTIVE_PROPOSAL_SCOPE_INVALID');
 return {tenant,project,producer,base:structuredClone(expected)};
}
// This is a metadata descriptor. Artifact refs are never image URLs or proof
// of user acceptance; pixel previews and Apply require separate server paths.
export function readPerspectiveProposal(value:unknown,tenant:string,project:string,producer:string,expected:V2VersionBinding):PerspectiveProposal{
 const scope=perspectiveProposalScope(tenant,project,producer,expected);
 const invalid=()=>{throw new Error('PERSPECTIVE_PROPOSAL_INVALID');};
 if(!fields(value,['schema_version','tenant_id','project_id','producer_task_id','result_id','result','base','working_image','changed_mask','report','canvas','status','requires_review']))invalid();
 const v=value as Record<string,unknown>;
 if(v.schema_version!==1||v.tenant_id!==scope.tenant||v.project_id!==scope.project||v.producer_task_id!==scope.producer||!uuid(v.result_id)||!base(v.base)||!ref(v.result)||!ref(v.working_image)||!ref(v.changed_mask)||!ref(v.report))invalid();
 const b=v.base as V2VersionBinding;
 if(b.asset_version_id!==scope.base.asset_version_id||b.canvas_hash!==scope.base.canvas_hash||b.source.artifact_id!==scope.base.source.artifact_id||b.source.sha256!==scope.base.source.sha256)invalid();
 if((v.status!=='pass'&&v.status!=='review')||v.requires_review!==(v.status==='review'))invalid();
 const c=v.canvas;
 if(!fields(c,['width','height','orientation','space_id','source_sha256'])||!Number.isSafeInteger(c.width)||!Number.isSafeInteger(c.height)||typeof c.width!=='number'||typeof c.height!=='number'||c.width<1||c.height<1||c.width*c.height>200_000_000||!Number.isInteger(c.orientation)||typeof c.orientation!=='number'||c.orientation!==1||typeof c.space_id!=='string'||!/^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,119}$/.test(c.space_id)||!sha(c.source_sha256))invalid();
 const image=v.working_image as ArtifactRef,refs=[v.result,v.changed_mask,v.report] as ArtifactRef[];
 if(new Set([...refs.map(r=>r.artifact_id),image.artifact_id]).size!==4||refs.some(r=>r.artifact_id===b.source.artifact_id))invalid();
 const identity=image.artifact_id===b.source.artifact_id;
 if(identity?(image.sha256!==b.source.sha256||v.status!=='pass'):(v.status!=='review'||(c as PerspectiveProposalCanvas).orientation!==1))invalid();
 return structuredClone(value) as PerspectiveProposal;
}
