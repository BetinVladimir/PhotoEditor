import {describe,it,expect} from 'vitest';
import {perspectiveProducerTask,readPerspectiveAttemptResult} from './perspective-result';
import type {OwnerAPI} from './owner-api';
import type {PerspectiveIntent} from './perspective-intent';
import type {PerspectiveProposal} from './perspective-proposal';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const body=():PerspectiveIntent=>({schema_version:1,snapshot_id:id(3),params:{schema_version:1,base:{asset_version_id:id(4),source:ref(5),canvas_hash:'b'.repeat(64)},strength:1,mode:'manual',corners:[{x:0,y:0},{x:1,y:0},{x:1,y:1},{x:0,y:1}],lines:null,max_crop_fraction:.5}});
const ticket={run_id:id(1),workflow_id:id(2),state:'queued' as const};
const proposal=():PerspectiveProposal=>({schema_version:1,tenant_id:id(6),project_id:id(7),producer_task_id:perspectiveProducerTask(id(2)),result_id:id(8),result:ref(9),base:body().params.base,working_image:ref(10),changed_mask:ref(11),report:ref(12),canvas:{width:100,height:80,orientation:1,space_id:'perspective:test',source_sha256:'c'.repeat(64)},status:'review',requires_review:true});
describe('perspective result workflow handoff',()=>{
 it('matches the UUIDv5 node-key ABI',()=>{expect(perspectiveProducerTask(id(2))).toBe('3991f76d-c34c-526a-b1f3-129ef07c4b04');expect(()=>perspectiveProducerTask('../bad')).toThrow();});
 it('keeps pending workflows out of result IO and refuses terminal failures',async()=>{
  let reads=0;let state='running';const api={get:async()=>({...ticket,state}),perspectiveProposal:async()=>{reads++;return proposal();}} as unknown as Pick<OwnerAPI,'get'|'perspectiveProposal'>;
  expect(await readPerspectiveAttemptResult(api,id(6),id(7),body(),ticket,new AbortController().signal)).toEqual({state:'pending',workflow_state:'running'});
  for(state of ['failed','cancelled','cancelling','completed_with_errors','unknown'])await expect(readPerspectiveAttemptResult(api,id(6),id(7),body(),ticket,new AbortController().signal)).rejects.toThrow();
  expect(reads).toBe(0);
 });
 it('binds adopted proposal to the derived producer and exact source',async()=>{
  let seen:unknown[]=[];const api={get:async()=>({...ticket,state:'waiting_review'}),perspectiveProposal:async(...args:unknown[])=>{seen=args;return proposal();}} as unknown as Pick<OwnerAPI,'get'|'perspectiveProposal'>;
  const controller=new AbortController(),result=await readPerspectiveAttemptResult(api,id(6),id(7),body(),ticket,controller.signal);
  expect(result.state).toBe('available');expect(seen).toEqual([id(7),perspectiveProducerTask(id(2)),body().params.base,controller.signal]);
 });
 it('refuses a foreign workflow and cancellation before lookup',async()=>{
  let calls=0;const api={get:async()=>{calls++;return {...ticket,run_id:id(13),state:'completed'};},perspectiveProposal:async()=>{throw new Error('must not read');}} as unknown as Pick<OwnerAPI,'get'|'perspectiveProposal'>;
  await expect(readPerspectiveAttemptResult(api,id(6),id(7),body(),ticket,new AbortController().signal)).rejects.toThrow('PERSPECTIVE_RESULT_SCOPE_CHANGED');
  const controller=new AbortController();controller.abort();await expect(readPerspectiveAttemptResult(api,id(6),id(7),body(),ticket,controller.signal)).rejects.toThrow('PERSPECTIVE_RESULT_CANCELLED');expect(calls).toBe(1);
 });
 it('refuses cancellation arriving during proposal IO',async()=>{
  const controller=new AbortController();const api={get:async()=>({...ticket,state:'completed'}),perspectiveProposal:async()=>{controller.abort();return proposal();}} as unknown as Pick<OwnerAPI,'get'|'perspectiveProposal'>;
  await expect(readPerspectiveAttemptResult(api,id(6),id(7),body(),ticket,controller.signal)).rejects.toThrow('PERSPECTIVE_RESULT_CANCELLED');
 });
});
