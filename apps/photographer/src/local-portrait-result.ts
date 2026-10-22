import type {ArtifactRef} from './catalog-types';
export type LocalPortraitResult={run_id:string;result_id:string;operation:'portrait.hair'|'portrait.clothing';base:ArtifactRef;candidate:ArtifactRef;review_id:null};
export type LocalPortraitAutoAppliedVersion={asset_id:string;project_id:string;asset_version_id:string;run_id:string;result_id:string;operation:LocalPortraitResult['operation'];source:ArtifactRef};
export type LocalPortraitDiscovery={run_id:string;workflow_id:string|null};
const uuid=(value:unknown):value is string=>typeof value==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(value)&&value!=='00000000-0000-0000-0000-000000000000';
const record=(value:unknown):value is Record<string,unknown>=>!!value&&typeof value==='object'&&!Array.isArray(value);
const keys=(value:Record<string,unknown>,names:string[])=>Object.keys(value).length===names.length&&names.every(name=>Object.hasOwn(value,name));
const ref=(value:unknown):value is ArtifactRef=>record(value)&&keys(value,['artifact_id','sha256'])&&uuid(value.artifact_id)&&typeof value.sha256==='string'&&/^[0-9a-f]{64}$/.test(value.sha256);
const operation=(value:unknown)=>value==='portrait.hair'||value==='portrait.clothing';
export function readLocalPortraitDiscovery(value:unknown,run:string):LocalPortraitDiscovery{
 if(!uuid(run)||!record(value)||!keys(value,['run_id','workflow_id'])||value.run_id!==run||value.workflow_id!==null&&!uuid(value.workflow_id))throw new Error('LOCAL_PORTRAIT_DISCOVERY_INVALID');return structuredClone(value) as LocalPortraitDiscovery;
}
export function readLocalPortraitResult(value:unknown,runID:string):LocalPortraitResult{
 if(!uuid(runID)||!record(value)||!keys(value,['run_id','result_id','operation','base','candidate','review_id'])||value.run_id!==runID||!uuid(value.result_id)||!operation(value.operation)||value.review_id!==null||!ref(value.base)||!ref(value.candidate)||value.base.artifact_id===value.candidate.artifact_id)throw new Error('LOCAL_PORTRAIT_RESULT_INVALID');
 return structuredClone(value) as unknown as LocalPortraitResult;
}
export function readLocalPortraitAutoAppliedVersion(value:unknown,selected:LocalPortraitResult):LocalPortraitAutoAppliedVersion{
 const owned=readLocalPortraitResult(selected,selected.run_id);
 if(!record(value)||!keys(value,['asset_id','project_id','asset_version_id','run_id','result_id','operation','source'])||!uuid(value.asset_id)||!uuid(value.project_id)||!uuid(value.asset_version_id)||value.run_id!==owned.run_id||value.result_id!==owned.result_id||value.operation!==owned.operation||!ref(value.source)||value.source.artifact_id!==owned.candidate.artifact_id||value.source.sha256!==owned.candidate.sha256)throw new Error('LOCAL_PORTRAIT_AUTO_APPLY_INVALID');
 return structuredClone(value) as unknown as LocalPortraitAutoAppliedVersion;
}
