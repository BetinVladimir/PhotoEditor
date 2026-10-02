import {canonicalJSON} from '@photoeditor/api-client';
import type {ShootSession,ShootSessionPolicy} from './capture-types';
import type {SceneBoard} from './scene-board';
export type SceneAnalysisRequest={expected_revision:string;selection_snapshot_id:string;policy:ShootSessionPolicy;manual_references:{previous_analysis_id:string;group_id:string;asset_version_id:string}[]};
/** Preserve displayed references in all groups while changing one; never edit the old plan. */
export function sceneReferenceRequest(board:SceneBoard,analysisId:string,session:ShootSession,snapshotId:string,groupId:string,versionId:string):SceneAnalysisRequest{
 const group=board.groups.find(group=>group.id===groupId);
 if(!group||!group.frames.some(frame=>frame.versionId===versionId)||session.state!=='open'||!analysisId||!snapshotId||!/^\d+$/.test(session.revision)||board.groups.length>5000)throw new Error('SCENE_REFERENCE_INVALID');
 const request:SceneAnalysisRequest={expected_revision:session.revision,selection_snapshot_id:snapshotId,policy:structuredClone(session.policy),manual_references:board.groups.map(group=>({previous_analysis_id:analysisId,group_id:group.id,asset_version_id:group.id===groupId?versionId:group.reference.versionId}))};
 if(new TextEncoder().encode(canonicalJSON(request)).byteLength>1048576)throw new Error('SCENE_REFERENCE_INVALID');
 return request;
}
/** Initial analysis uses automatic references and the current frozen session policy. */
export function initialSceneAnalysisRequest(session:ShootSession,snapshotId:string):SceneAnalysisRequest{
 if(session.state!=='open'||!snapshotId||!/^[1-9][0-9]*$/.test(session.revision))throw new Error('SCENE_ANALYSIS_INVALID');
 const request:SceneAnalysisRequest={expected_revision:session.revision,selection_snapshot_id:snapshotId,policy:structuredClone(session.policy),manual_references:[]};
 if(new TextEncoder().encode(canonicalJSON(request)).byteLength>1048576)throw new Error('SCENE_ANALYSIS_INVALID');
 return request;
}
