import type {OwnerAPI} from './owner-api';
import {renderBatchTicket,type RenderBatchTicket} from './render-batch';
import {readRenderRunStatus} from './render-status';

/** Cancels the admitted parent run. HTTP abort alone is not server cancellation. */
export async function cancelEditorRender(api:Pick<OwnerAPI,'cancelRun'>,ticket:RenderBatchTicket,signal:AbortSignal){
 const owned=renderBatchTicket(ticket);signal.throwIfAborted();
 const response=await api.cancelRun(owned.run_id,signal);signal.throwIfAborted();
 return readRenderRunStatus(response,owned.run_id);
}
