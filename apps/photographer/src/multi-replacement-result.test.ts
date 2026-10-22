import {describe,it,expect,vi} from 'vitest';
import type {OwnerAPI} from './owner-api';
import {multiNamespacedUUID,multiWorkflowID} from './multi-result';
import {freezeMultiReplacementScope,readMultiReplacementScope,readMultiReplacementResult,type MultiReplacementResultScope} from './multi-replacement-result';
import type {MultiOperation} from './multi-intent';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
function fixture(operation:MultiOperation){
 const tenant=id(1),project=id(2),review=id(3),decisionHash='b'.repeat(64),originalRun=id(4),originalTask=id(5);
 const namespace=multiNamespacedUUID(tenant,`V29.NegativeReplacement/v2:${project}:${review}:${decisionHash}`);
 const run=multiNamespacedUUID(namespace,`run:${originalRun}:${originalTask}`),task=multiNamespacedUUID(namespace,`task:${originalTask}`);
 const scope:MultiReplacementResultScope={tenant,project,review,decisionHash,originalRun,originalTask,run,task,workflow:multiWorkflowID(run),operation,intent:{schema_version:1,snapshot_id:id(6),params:{schema_version:1,frames:[{asset_version_id:id(7),source:ref(8),canvas_hash:'c'.repeat(64)},{asset_version_id:id(9),source:ref(10),canvas_hash:'d'.repeat(64)}],reference_ordinal:1,max_pixels:10000,calibration:ref(11),...(operation==='optical.panorama'?{projection:'planar' as const}:{motion_policy:'review' as const})}}};
 const proposal={schema_version:1,tenant_id:tenant,project_id:project,producer_task_id:task,operation,result_id:id(12),result:ref(13),sources:structuredClone(scope.intent.params.frames),reference_ordinal:1,working_image:ref(14),changed_mask:ref(15),report:ref(16),registrations:[ref(17),ref(18)],canvas:{width:100,height:100,orientation:1,space_id:'multi:'+'c'.repeat(32),source_sha256:'d'.repeat(64)},status:'review',requires_review:true,reason:operation.slice(8).toUpperCase()+'_REVIEW;motion_fraction=0'};
 return {scope,proposal};
}
for(const operation of ['optical.panorama','optical.focus_stack'] as const)describe(operation+' replacement result',()=>{
 it('loads the replacement task with owned mapped frames',async()=>{
  const {scope,proposal}=fixture(operation);let finish!:(v:unknown)=>void;
  const api={get:vi.fn(()=>new Promise(resolve=>{finish=resolve;})),multiProposal:vi.fn(async()=>proposal)} as unknown as Pick<OwnerAPI,'get'|'multiProposal'>;
  const saved=structuredClone(scope.intent),controller=new AbortController();
  const pending=readMultiReplacementResult(api,scope,controller.signal);scope.intent.params.frames.reverse();
  finish({run_id:scope.run,workflow_id:scope.workflow,state:'completed'});
  expect((await pending).state).toBe('available');expect(api.multiProposal).toHaveBeenCalledWith(operation,scope.project,scope.task,saved,controller.signal);
 });
 it('rejects every substituted identity before network access',async()=>{
  const {scope}=fixture(operation),get=vi.fn(),api={get,multiProposal:vi.fn()} as unknown as Pick<OwnerAPI,'get'|'multiProposal'>;
  for(const key of ['tenant','project','review','originalRun','originalTask','run','workflow','task'] as const){
   await expect(readMultiReplacementResult(api,{...scope,[key]:id(99)},new AbortController().signal)).rejects.toThrow('MULTI_REPLACEMENT_SCOPE_INVALID');
  }
  expect(get).not.toHaveBeenCalled();expect(()=>freezeMultiReplacementScope({...scope,decisionHash:'z'.repeat(64)})).toThrow();
 });
 it('does not read proposals while pending or after terminal failure',async()=>{
  const {scope}=fixture(operation);let state='running';const proposal=vi.fn();
  const api={get:async()=>({run_id:scope.run,workflow_id:scope.workflow,state}),multiProposal:proposal} as unknown as Pick<OwnerAPI,'get'|'multiProposal'>;
  expect((await readMultiReplacementResult(api,scope,new AbortController().signal)).state).toBe('pending');
  for(state of ['failed','cancelled','completed_with_errors'])expect(await readMultiReplacementResult(api,scope,new AbortController().signal)).toEqual({state:'terminal',workflow_state:state});
  state='cancelling';expect(await readMultiReplacementResult(api,scope,new AbortController().signal)).toEqual({state:'pending',workflow_state:'cancelling'});
  state='unknown';await expect(readMultiReplacementResult(api,scope,new AbortController().signal)).rejects.toThrow('MULTI_RESULT_UNAVAILABLE');
  expect(proposal).not.toHaveBeenCalled();
 });
 it('rejects foreign workflow status and cancellation after delivery',async()=>{
  const {scope,proposal}=fixture(operation);const controller=new AbortController();
  let foreign=true;const api={get:async()=>({run_id:foreign?id(99):scope.run,workflow_id:scope.workflow,state:'completed'}),multiProposal:async()=>{controller.abort();return proposal;}} as unknown as Pick<OwnerAPI,'get'|'multiProposal'>;
  await expect(readMultiReplacementResult(api,scope,controller.signal)).rejects.toThrow('MULTI_RESULT_SCOPE_CHANGED');foreign=false;
  await expect(readMultiReplacementResult(api,scope,controller.signal)).rejects.toThrow('MULTI_RESULT_CANCELLED');
 });
 it('owns a closed discovery packet and rejects foreign operation',()=>{
  const {scope}=fixture(operation),wire={tenant_id:scope.tenant,project_id:scope.project,review_id:scope.review,decision_hash:scope.decisionHash,original_run_id:scope.originalRun,original_task_id:scope.originalTask,run_id:scope.run,workflow_id:scope.workflow,task_id:scope.task,operation,intent:scope.intent};
  const parse=(v:unknown)=>readMultiReplacementScope(v,scope.tenant,scope.project,scope.review,scope.originalTask,scope.decisionHash,operation);
  const parsed=parse(wire);wire.intent.params.frames.reverse();expect(parsed.intent.params.frames[0].asset_version_id).toBe(id(7));
  expect(()=>parse({...wire,extra:true})).toThrow();expect(()=>parse({...wire,operation:'optical.perspective'})).toThrow();
 });
});
