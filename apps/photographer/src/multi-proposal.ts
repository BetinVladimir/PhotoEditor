import type {ArtifactRef,V2VersionBinding} from './catalog-types';
import type {PerspectiveProposalCanvas} from './perspective-proposal';
import {freezeMultiIntent,multiUUID,type MultiIntent,type MultiOperation} from './multi-intent';
export type MultiProposal={schema_version:1;tenant_id:string;project_id:string;producer_task_id:string;operation:MultiOperation;result_id:string;result:ArtifactRef;sources:V2VersionBinding[];reference_ordinal:number;working_image:ArtifactRef;changed_mask:ArtifactRef;report:ArtifactRef;registrations:ArtifactRef[];canvas:PerspectiveProposalCanvas;status:'review';requires_review:true;reason:string};
const fields=(v:unknown,keys:string[]):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.prototype.hasOwnProperty.call(v,k));
const sha=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
const ref=(v:unknown):v is ArtifactRef=>fields(v,['artifact_id','sha256'])&&multiUUID(v.artifact_id)&&sha(v.sha256);
export function freezeMultiProposalScope(tenant:string,project:string,producer:string,operation:MultiOperation,intent:MultiIntent){
 if(!multiUUID(tenant)||!multiUUID(project)||!multiUUID(producer))throw new Error('MULTI_PROPOSAL_SCOPE_INVALID');
 return {tenant,project,producer,operation,intent:freezeMultiIntent(operation,intent)};
}
/** A descriptor is not an image URL, independent quality PASS or Apply approval. */
export function readMultiProposal(value:unknown,tenant:string,project:string,producer:string,operation:MultiOperation,intent:MultiIntent):MultiProposal{
 const s=freezeMultiProposalScope(tenant,project,producer,operation,intent),invalid=():never=>{throw new Error('MULTI_PROPOSAL_INVALID');};
 if(!fields(value,['schema_version','tenant_id','project_id','producer_task_id','operation','result_id','result','sources','reference_ordinal','working_image','changed_mask','report','registrations','canvas','status','requires_review','reason']))invalid();
 const v=value as Record<string,unknown>;
 if(v.schema_version!==1||v.tenant_id!==tenant||v.project_id!==project||v.producer_task_id!==producer||v.operation!==operation||!multiUUID(v.result_id)||v.status!=='review'||v.requires_review!==true||v.reference_ordinal!==s.intent.params.reference_ordinal)invalid();
 if(!Array.isArray(v.sources)||v.sources.length!==s.intent.params.frames.length||!Array.isArray(v.registrations)||v.registrations.length!==v.sources.length)invalid();
 const sources=v.sources as unknown[],registrations=v.registrations as unknown[];
 for(let i=0;i<sources.length;i++){
  const actual=sources[i],expected=s.intent.params.frames[i];
  if(!fields(actual,['asset_version_id','source','canvas_hash'])||actual.asset_version_id!==expected.asset_version_id||actual.canvas_hash!==expected.canvas_hash||!ref(actual.source)||actual.source.artifact_id!==expected.source.artifact_id||actual.source.sha256!==expected.source.sha256)invalid();
 }
 const all=[v.result,v.working_image,v.changed_mask,v.report,...registrations];if(!all.every(ref))invalid();
 const refs=all as ArtifactRef[],used=new Set(s.intent.params.frames.map(f=>f.source.artifact_id));used.add(s.intent.params.calibration.artifact_id);
 for(const r of refs){if(used.has(r.artifact_id))invalid();used.add(r.artifact_id);}
 const c=v.canvas;
 if(!fields(c,['width','height','orientation','space_id','source_sha256'])||typeof c.width!=='number'||typeof c.height!=='number'||!Number.isSafeInteger(c.width)||!Number.isSafeInteger(c.height)||c.width<1||c.height<1||c.width*c.height>s.intent.params.max_pixels||c.orientation!==1||typeof c.space_id!=='string'||!/^multi:[0-9a-f]{32}$/.test(c.space_id)||!sha(c.source_sha256))invalid();
 const prefix=operation.slice('optical.'.length).toUpperCase()+'_REVIEW;motion_fraction=';
 if(typeof v.reason!=='string'||v.reason.length>256||!v.reason.startsWith(prefix))invalid();
 const parts=(v.reason as string).slice(prefix.length).split(';'),number=parts[0];
 if(parts.length>2||(parts.length===2&&parts[1]!=='REGISTRATION_INCONSISTENT')||!/^\d+(?:\.\d+)?(?:e[+-]?\d+)?$/i.test(number)||!Number.isFinite(Number(number))||Number(number)<0||Number(number)>1)invalid();
 return structuredClone(value) as MultiProposal;
}
