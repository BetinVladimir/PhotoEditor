import type {ArtifactRef,V2VersionBinding} from './catalog-types';
export type RenderFreezeRequest={document_id:string;expected_revision:string;assume_profile:null|'srgb_v1'|'linear_srgb_v1'|'work_rec2020_gamma24_v1'};
export type RenderFreezeResult={document_id:string;draft_document_id:string;draft_revision:string;source:V2VersionBinding;recipe:ArtifactRef};
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const sha=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
function fields(v:unknown,keys:string[]):v is Record<string,unknown>{return !!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.prototype.hasOwnProperty.call(v,k));}
function ref(v:unknown):v is ArtifactRef{return fields(v,['artifact_id','sha256'])&&uuid(v.artifact_id)&&sha(v.sha256);}
export function renderFreezeRequest(v:RenderFreezeRequest):RenderFreezeRequest{
 if(!fields(v,['document_id','expected_revision','assume_profile'])||!uuid(v.document_id)||typeof v.expected_revision!=='string'||! /^[1-9][0-9]{0,18}$/.test(v.expected_revision)||BigInt(v.expected_revision)>9223372036854775807n||(v.assume_profile!==null&&!['srgb_v1','linear_srgb_v1','work_rec2020_gamma24_v1'].includes(v.assume_profile)))throw new Error('RENDER_FREEZE_INVALID');
 return {document_id:v.document_id,expected_revision:v.expected_revision,assume_profile:v.assume_profile};
}
export function renderFreezeResult(v:unknown,request:RenderFreezeRequest):RenderFreezeResult{
 if(!fields(v,['document_id','draft_document_id','draft_revision','source','recipe'])||!uuid(v.document_id)||v.document_id===request.document_id||v.draft_document_id!==request.document_id||v.draft_revision!==request.expected_revision||!fields(v.source,['asset_version_id','source','canvas_hash'])||!uuid(v.source.asset_version_id)||!ref(v.source.source)||!sha(v.source.canvas_hash)||!ref(v.recipe))throw new Error('RENDER_FREEZE_RESULT_INVALID');
 return {document_id:v.document_id,draft_document_id:request.document_id,draft_revision:request.expected_revision,source:{asset_version_id:v.source.asset_version_id,source:{...v.source.source},canvas_hash:v.source.canvas_hash},recipe:{...v.recipe}};
}
