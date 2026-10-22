import {describe,it,expect} from 'vitest';
import {multiWorkflowID,multiProducerTask,multiTicketProducer,readMultiAttemptResult} from './multi-result';
import type {OwnerAPI} from './owner-api';
import type {MultiIntent} from './multi-intent';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const body=():MultiIntent=>({schema_version:1,snapshot_id:id(30),params:{schema_version:1,frames:[{asset_version_id:id(20),source:ref(21),canvas_hash:'b'.repeat(64)},{asset_version_id:id(22),source:ref(23),canvas_hash:'c'.repeat(64)}],reference_ordinal:1,max_pixels:10000,calibration:ref(24),projection:'planar'}});
const ticket={run_id:id(1),workflow_id:multiWorkflowID(id(1)),state:'queued' as const};
const proposal=()=>({schema_version:1,tenant_id:id(6),project_id:id(7),producer_task_id:multiProducerTask(ticket.workflow_id,'optical.panorama'),operation:'optical.panorama',result_id:id(8),result:ref(9),sources:body().params.frames,reference_ordinal:1,working_image:ref(10),changed_mask:ref(11),report:ref(12),registrations:[ref(13),ref(14)],canvas:{width:100,height:100,orientation:1,space_id:'multi:'+'c'.repeat(32),source_sha256:'d'.repeat(64)},status:'review',requires_review:true,reason:'PANORAMA_REVIEW;motion_fraction=0'});
describe('multi durable result handoff',()=>{
 it('keeps pending and failed workflows out of result reads',async()=>{
  let reads=0,state='running';const api={get:async()=>({...ticket,state}),multiProposal:async()=>{reads++;return proposal();}} as unknown as Pick<OwnerAPI,'get'|'multiProposal'>;
  expect(await readMultiAttemptResult(api,id(6),id(7),'optical.panorama',body(),ticket,new AbortController().signal)).toEqual({state:'pending',workflow_state:'running'});
  for(state of ['failed','cancelled','completed_with_errors'])expect(await readMultiAttemptResult(api,id(6),id(7),'optical.panorama',body(),ticket,new AbortController().signal)).toEqual({state:'terminal',workflow_state:state});
  state='cancelling';expect(await readMultiAttemptResult(api,id(6),id(7),'optical.panorama',body(),ticket,new AbortController().signal)).toEqual({state:'pending',workflow_state:'cancelling'});
  for(state of ['unknown'])await expect(readMultiAttemptResult(api,id(6),id(7),'optical.panorama',body(),ticket,new AbortController().signal)).rejects.toThrow('MULTI_RESULT_UNAVAILABLE');expect(reads).toBe(0);
 });
 it('binds completed result to exact producer and frozen frame order',async()=>{
  const intent=body();let finish!:(v:unknown)=>void;
  const api={get:()=>new Promise(resolve=>{finish=resolve;}),multiProposal:async()=>proposal()} as unknown as Pick<OwnerAPI,'get'|'multiProposal'>;
  const pending=readMultiAttemptResult(api,id(6),id(7),'optical.panorama',intent,ticket,new AbortController().signal);
  intent.params.frames.reverse();finish({...ticket,state:'waiting_review'});expect((await pending).state).toBe('available');
 });
 it('refuses forged ticket before transport and foreign workflow status',async()=>{
  let calls=0;const api={get:async()=>{calls++;return {...ticket,run_id:id(40),state:'completed'};},multiProposal:async()=>proposal()} as unknown as Pick<OwnerAPI,'get'|'multiProposal'>;
  await expect(readMultiAttemptResult(api,id(6),id(7),'optical.panorama',body(),{...ticket,workflow_id:id(2)},new AbortController().signal)).rejects.toThrow('MULTI_RESULT_SCOPE_INVALID');expect(calls).toBe(0);
  await expect(readMultiAttemptResult(api,id(6),id(7),'optical.panorama',body(),ticket,new AbortController().signal)).rejects.toThrow('MULTI_RESULT_SCOPE_CHANGED');
 });
 it('refuses cancellation during late result delivery',async()=>{
  const controller=new AbortController(),api={get:async()=>({...ticket,state:'completed'}),multiProposal:async()=>{controller.abort();return proposal();}} as unknown as Pick<OwnerAPI,'get'|'multiProposal'>;
  await expect(readMultiAttemptResult(api,id(6),id(7),'optical.panorama',body(),ticket,controller.signal)).rejects.toThrow('MULTI_RESULT_CANCELLED');
 });
 it('separates producer IDs by operation',()=>{expect(multiProducerTask(ticket.workflow_id,'optical.panorama')).not.toBe(multiProducerTask(ticket.workflow_id,'optical.focus_stack'));expect(()=>multiWorkflowID('../bad')).toThrow();});
});

describe('focus-stack result handoff',()=>{
 it('uses focus operation/task and preserves motion policy on result request',async()=>{
  const original=body(),{projection:_,...common}=original.params as typeof original.params&{projection:string};
  const intent:MultiIntent={...original,params:{...common,motion_policy:'review'}};
  const value={...proposal(),operation:'optical.focus_stack',producer_task_id:multiProducerTask(ticket.workflow_id,'optical.focus_stack'),reason:'FOCUS_STACK_REVIEW;motion_fraction=0'};
  let seen:unknown[]=[];const api={get:async()=>({...ticket,state:'completed'}),multiProposal:async(...args:unknown[])=>{seen=args;return value;}} as unknown as Pick<OwnerAPI,'get'|'multiProposal'>;
  const controller=new AbortController();expect((await readMultiAttemptResult(api,id(6),id(7),'optical.focus_stack',intent,ticket,controller.signal)).state).toBe('available');
  expect(seen).toEqual(['optical.focus_stack',id(7),value.producer_task_id,intent,controller.signal]);
 });
});

it('derives recovery producer without a live proposal and refuses a forged acknowledgement',()=>{
 expect(multiTicketProducer('optical.panorama',ticket)).toBe(multiProducerTask(ticket.workflow_id,'optical.panorama'));
 expect(multiTicketProducer('optical.focus_stack',ticket)).toBe(multiProducerTask(ticket.workflow_id,'optical.focus_stack'));
 expect(()=>multiTicketProducer('optical.panorama',{...ticket,workflow_id:id(90)})).toThrow('MULTI_RESULT_SCOPE_INVALID');
});
