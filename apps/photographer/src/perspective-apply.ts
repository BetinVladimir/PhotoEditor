import {readPerspectiveQuality,type PerspectiveQuality} from './perspective-quality';
import type {ArtifactRef} from './catalog-types';
export type PerspectiveApplyDecision={schema_version:1;result_id:string;root:ArtifactRef;quality_run_id:string;quality_result_id:string;quality_report:ArtifactRef;confirm_geometry_reset:true;accept_quality_warnings:boolean};
export type PerspectiveApplied={asset_version_id:string;parent_asset_version_id:string;source:ArtifactRef};
export function preparePerspectiveApply(value:PerspectiveQuality,confirmGeometryReset:boolean,acceptQualityWarnings:boolean):PerspectiveApplyDecision{
 const current=readPerspectiveQuality(value,value.proposal);
 if(confirmGeometryReset!==true||typeof acceptQualityWarnings!=='boolean'||current.quality.status==='fail'||current.quality.status==='review'&&!acceptQualityWarnings||current.proposal.working_image.artifact_id===current.proposal.base.source.artifact_id)throw new Error('PERSPECTIVE_APPLY_INVALID');
 return structuredClone({schema_version:1,result_id:current.proposal.result_id,root:current.proposal.result,quality_run_id:current.quality_run_id,quality_result_id:current.quality_result_id,quality_report:current.quality_report,confirm_geometry_reset:true,accept_quality_warnings:acceptQualityWarnings});
}
export function readPerspectiveApplied(value:unknown,current:PerspectiveQuality):PerspectiveApplied{
 const fail=()=>{throw new Error('PERSPECTIVE_APPLY_RESULT_INVALID');};
 if(!value||typeof value!=='object'||Array.isArray(value)||Object.keys(value).length!==3||!['asset_version_id','parent_asset_version_id','source'].every(k=>Object.hasOwn(value,k)))fail();
 const v=value as PerspectiveApplied;
 if(typeof v.asset_version_id!=='string'||!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v.asset_version_id)||v.asset_version_id==='00000000-0000-0000-0000-000000000000'||v.asset_version_id===current.proposal.base.asset_version_id||v.parent_asset_version_id!==current.proposal.base.asset_version_id)fail();
 if(!v.source||Object.keys(v.source).length!==2||v.source.artifact_id!==current.proposal.working_image.artifact_id||v.source.sha256!==current.proposal.working_image.sha256)fail();
 return structuredClone(v);
}
