import type {ArtifactRef,V2VersionBinding} from './catalog-types';
import type {RenderFreezeResult} from './render-freeze';
export type RenderBatchSubmission={input_snapshot_id:string;policy_sha:string;selection:{source:V2VersionBinding;recipe:ArtifactRef}[]};
export type RenderBatchTicket={run_id:string;state:'queued'|'running'|'waiting_review';child_run_ids:string[]};
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const sha=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
function fields(v:unknown,keys:string[]):v is Record<string,unknown>{return !!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.prototype.hasOwnProperty.call(v,k));}
function ref(v:unknown):v is ArtifactRef{return fields(v,['artifact_id','sha256'])&&uuid(v.artifact_id)&&sha(v.sha256);}
export function isRenderSourceBinding(v:unknown):v is V2VersionBinding{return fields(v,['asset_version_id','source','canvas_hash'])&&uuid(v.asset_version_id)&&ref(v.source)&&sha(v.canvas_hash);}
export function renderBatchSubmission(v:RenderBatchSubmission):RenderBatchSubmission{
 if(!fields(v,['input_snapshot_id','policy_sha','selection'])||!uuid(v.input_snapshot_id)||!sha(v.policy_sha)||!Array.isArray(v.selection)||v.selection.length<1||v.selection.length>5000)throw new Error('RENDER_BATCH_INVALID');
 const seen=new Set<string>();
 for(const item of v.selection){if(!fields(item,['source','recipe'])||!isRenderSourceBinding(item.source)||!ref(item.recipe)||item.recipe.artifact_id.toLowerCase()===item.source.source.artifact_id.toLowerCase()||seen.has(item.source.asset_version_id.toLowerCase()))throw new Error('RENDER_BATCH_INVALID');seen.add(item.source.asset_version_id.toLowerCase());}
 return structuredClone(v);
}
/** Ordered immutable freeze results are the only per-image inputs. */
export function renderBatchFromSnapshots(snapshot:string,policy:string,items:readonly RenderFreezeResult[]):RenderBatchSubmission{
 return renderBatchSubmission({input_snapshot_id:snapshot,policy_sha:policy,selection:items.map(item=>({source:item.source,recipe:item.recipe}))});
}
export function renderBatchTicket(v:unknown):RenderBatchTicket{
 if(!fields(v,['run_id','state','child_run_ids'])||!uuid(v.run_id)||!['queued','running','waiting_review'].includes(v.state as string)||!Array.isArray(v.child_run_ids)||v.child_run_ids.length<1||v.child_run_ids.length>5000||!v.child_run_ids.every(uuid)||v.child_run_ids.some(id=>id.toLowerCase()===(v.run_id as string).toLowerCase())||new Set(v.child_run_ids.map(id=>id.toLowerCase())).size!==v.child_run_ids.length)throw new Error('RENDER_BATCH_TICKET_INVALID');
 return {run_id:v.run_id,state:v.state as RenderBatchTicket['state'],child_run_ids:[...v.child_run_ids]};
}
