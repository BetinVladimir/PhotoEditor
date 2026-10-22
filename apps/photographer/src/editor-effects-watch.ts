import type {OwnerAPI} from './owner-api';
import {submitEditorEffectsAttempt,type EditorEffectsAttempt} from './editor-effects-attempt';
import {readRenderImages,type RenderFrameResult} from './editor-render-results';
import {readRenderRunStatus} from './render-status';
import type {EditorEffectsStatus} from './editor-effects-contract';

/** The build and seal are never presented as the final render. Recovery of an
 * accepted command reads status only, including when the final run is absent. */
export async function observeEditorEffects(api:Pick<OwnerAPI,'queueEditorEffects'|'editorEffectsStatus'|'run'|'runOutputs'>,attempt:EditorEffectsAttempt,signal:AbortSignal,current:()=>boolean):Promise<{status:EditorEffectsStatus;frames:RenderFrameResult[]}>{
 if(!attempt.accepted)throw new Error('EDITOR_EFFECT_NOT_ACCEPTED');
 const check=()=>{signal.throwIfAborted();if(!current())throw new Error('EDITOR_EFFECT_SCOPE_EXPIRED');};
 const recovered=await submitEditorEffectsAttempt(api,attempt,signal,current);check();
 const status=recovered.status;if(!status)throw new Error('EDITOR_EFFECT_STATUS_REQUIRED');
 if(status.state==='failed'||!status.final_run_id)return {status,frames:[]};
 const run=readRenderRunStatus(await api.run(status.final_run_id,signal),status.final_run_id,'v2');check();
 const images=['completed','completed_with_errors','waiting_review'].includes(run.status)?readRenderImages(await api.runOutputs(status.final_run_id,signal)):[];check();
 return {status,frames:[{run:status.final_run_id,status:run.status,images,...(run.presentation?{presentation:run.presentation}:{})}]};
}
