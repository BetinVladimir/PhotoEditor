import {canonicalJSON} from '@photoeditor/api-client';
import {readRenderPolicy} from './render-policy';
import type {OwnerAPI} from './owner-api';
import type {RenderFreezeResult} from './render-freeze';
import {renderBatchFromSnapshots,renderBatchSubmission} from './render-batch';
import {renderInputSnapshotRequest} from './render-input-snapshot';

/** policySHA must be obtained from trusted deployment policy discovery.
 * This command never invents policy/model/runtime choices. Keep the returned
 * attempt for retries of either stage, including a lost queue response. */
export function prepareEditorRenderSubmission(frozen:RenderFreezeResult,policySHA:string,snapshotKey:string,renderKey:string){
 if(!snapshotKey||!renderKey||snapshotKey.length>128||renderKey.length>128||!/^[!-~]+$/.test(snapshotKey)||!/^[!-~]+$/.test(renderKey))throw new Error('IDEMPOTENCY_KEY_REQUIRED');
 // Freeze document identity is used only for local structural validation; the
 // submitted inventory ID always comes from createRenderInputSnapshot below.
 const checked=renderBatchFromSnapshots(frozen.document_id,policySHA,[frozen]);
 return {policySHA,snapshotKey,renderKey,selection:checked.selection,input:renderInputSnapshotRequest({sources:checked.selection.map(item=>item.source)})};
}
/** Revalidate persisted commands before any transport mutation. */
export function readEditorRenderSubmission(value:unknown,version?:string):ReturnType<typeof prepareEditorRenderSubmission>{
 if(!value||typeof value!=='object'||Array.isArray(value))throw new Error('RENDER_COMMAND_INVALID');
 const v=value as Record<string,unknown>,keys=['policySHA','snapshotKey','renderKey','selection','input'];
 if(Object.keys(v).length!==keys.length||!keys.every(key=>Object.hasOwn(v,key))||typeof v.policySHA!=='string'||!Array.isArray(v.selection)||v.selection.length!==1)throw new Error('RENDER_COMMAND_INVALID');
 for(const key of ['snapshotKey','renderKey'])if(typeof v[key]!=='string'||!(v[key] as string).length||(v[key] as string).length>128||!/^[!-~]+$/.test(v[key] as string))throw new Error('IDEMPOTENCY_KEY_REQUIRED');
 // The source version is only a local UUID validation anchor. The submitted
 // inventory ID is still obtained from the server, never from this anchor.
 const anchor=(v.selection[0] as {source?:{asset_version_id?:string}}|null)?.source?.asset_version_id;
 const checked=renderBatchSubmission({input_snapshot_id:anchor as string,policy_sha:v.policySHA,selection:v.selection});
 if(version!==undefined&&checked.selection[0].source.asset_version_id.toLowerCase()!==version.toLowerCase())throw new Error('RENDER_COMMAND_SOURCE_CHANGED');
 const input=renderInputSnapshotRequest(v.input as Parameters<typeof renderInputSnapshotRequest>[0]);
 const expected=renderInputSnapshotRequest({sources:checked.selection.map(item=>item.source)});
 if(canonicalJSON(input)!==canonicalJSON(expected))throw new Error('RENDER_COMMAND_INPUT_CHANGED');
 return {policySHA:checked.policy_sha,snapshotKey:v.snapshotKey as string,renderKey:v.renderKey as string,selection:checked.selection,input};
}

export async function submitEditorRender(api:Pick<OwnerAPI,'createRenderInputSnapshot'|'queueRenderBatch'>,project:string,attempt:ReturnType<typeof prepareEditorRenderSubmission>,signal:AbortSignal){
 const owned=readEditorRenderSubmission(attempt);signal.throwIfAborted();
 const inventory=await api.createRenderInputSnapshot(project,owned.input,owned.snapshotKey,signal);signal.throwIfAborted();
 const ticket=await api.queueRenderBatch(project,{input_snapshot_id:inventory.id,policy_sha:owned.policySHA,selection:owned.selection},owned.renderKey,signal);signal.throwIfAborted();return ticket;
}

/** Fetch policy before retaining the immutable submission attempt. Retries of a
 * prepared command use that attempt rather than silently selecting a new SHA. */
export async function prepareEditorRenderFromServerPolicy(api:Pick<OwnerAPI,'renderPolicy'>,project:string,frozen:RenderFreezeResult,snapshotKey:string,renderKey:string,signal:AbortSignal){
 const owned=structuredClone(frozen);signal.throwIfAborted();
 const policy=readRenderPolicy(await api.renderPolicy(project,signal));signal.throwIfAborted();
 return prepareEditorRenderSubmission(owned,policy.policy_sha,snapshotKey,renderKey);
}
