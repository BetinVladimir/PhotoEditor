import {it,expect,vi} from 'vitest';
import {readPerspectiveReplacementScope,readPerspectiveReplacementResult,type PerspectiveReplacementResultScope} from './perspective-replacement-result';
import type {OwnerAPI} from './owner-api';
const scope=():PerspectiveReplacementResultScope=>({"tenant": "00000000-0000-4000-8000-000000000001", "project": "00000000-0000-4000-8000-000000000002", "review": "00000000-0000-4000-8000-000000000003", "decisionHash": "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", "originalRun": "00000000-0000-4000-8000-000000000004", "originalTask": "00000000-0000-4000-8000-000000000005", "run": "dcbeea30-9f23-5c33-90e4-2a77ea6d0929", "workflow": "de3f97de-40fd-593d-bf50-7922818153bd", "task": "843945bd-0c83-527b-a18c-000ccb167137", "base": {"asset_version_id": "00000000-0000-4000-8000-000000000006", "source": {"artifact_id": "00000000-0000-4000-8000-000000000007", "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, "canvas_hash": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}});
const api=(state:string)=>({get:vi.fn().mockResolvedValue({run_id:scope().run,workflow_id:scope().workflow,state}),perspectiveProposal:vi.fn()});
it('matches replacement identity ABI and waits without proposal IO',async()=>{
 const service=api('running');
 expect(await readPerspectiveReplacementResult(service as unknown as Pick<OwnerAPI,'get'|'perspectiveProposal'>,scope(),new AbortController().signal)).toEqual({state:'pending',workflow_state:'running'});
 expect(service.perspectiveProposal).not.toHaveBeenCalled();
});
it('rejects substituted identity before IO',async()=>{
 for(const field of ['tenant','project','review','originalRun','originalTask','run','workflow','task','decisionHash'] as const){
  const body=scope();body[field]=field==='decisionHash'?'e'.repeat(64):'00000000-0000-4000-8000-000000000099';const service=api('completed');
  await expect(readPerspectiveReplacementResult(service as unknown as Pick<OwnerAPI,'get'|'perspectiveProposal'>,body,new AbortController().signal)).rejects.toThrow('PERSPECTIVE_REPLACEMENT_SCOPE_INVALID');
  expect(service.get).not.toHaveBeenCalled();
 }
});
it('loads exact replacement producer with owned mapped base',async()=>{
 const body=scope(),service=api('waiting_review');
 const proposal={schema_version:1,tenant_id:body.tenant,project_id:body.project,producer_task_id:body.task,result_id:'00000000-0000-4000-8000-000000000010',result:{artifact_id:'00000000-0000-4000-8000-000000000011',sha256:'c'.repeat(64)},base:structuredClone(body.base),working_image:{artifact_id:'00000000-0000-4000-8000-000000000012',sha256:'c'.repeat(64)},changed_mask:{artifact_id:'00000000-0000-4000-8000-000000000013',sha256:'c'.repeat(64)},report:{artifact_id:'00000000-0000-4000-8000-000000000014',sha256:'c'.repeat(64)},canvas:{width:4,height:3,orientation:1,space_id:'perspective:test',source_sha256:'c'.repeat(64)},status:'review',requires_review:true};
 service.get.mockImplementation(async()=>{body.base.canvas_hash='f'.repeat(64);return {run_id:body.run,workflow_id:body.workflow,state:'waiting_review'};});service.perspectiveProposal.mockResolvedValue(proposal);
 const signal=new AbortController().signal;
 const result=await readPerspectiveReplacementResult(service as unknown as Pick<OwnerAPI,'get'|'perspectiveProposal'>,body,signal);
 expect(result.state).toBe('available');expect(service.perspectiveProposal).toHaveBeenCalledWith(body.project,body.task,scope().base,signal);
});
it('rejects failed workflows and late cancellation',async()=>{
 for(const state of ['failed','cancelled','completed_with_errors']){
  const service=api(state);expect(await readPerspectiveReplacementResult(service as unknown as Pick<OwnerAPI,'get'|'perspectiveProposal'>,scope(),new AbortController().signal)).toEqual({state:'terminal',workflow_state:state});expect(service.perspectiveProposal).not.toHaveBeenCalled();
 }
 const unknown=api('unknown');await expect(readPerspectiveReplacementResult(unknown as unknown as Pick<OwnerAPI,'get'|'perspectiveProposal'>,scope(),new AbortController().signal)).rejects.toThrow('PERSPECTIVE_RESULT_UNAVAILABLE');
 const controller=new AbortController(),service=api('completed');service.get.mockImplementation(async()=>{controller.abort();return {run_id:scope().run,workflow_id:scope().workflow,state:'completed'};});
 await expect(readPerspectiveReplacementResult(service as unknown as Pick<OwnerAPI,'get'|'perspectiveProposal'>,scope(),controller.signal)).rejects.toThrow('PERSPECTIVE_RESULT_CANCELLED');expect(service.perspectiveProposal).not.toHaveBeenCalled();
});

it('decodes only exact current discovery scope and owns the mapped base',()=>{
 const v=scope();const wire={tenant_id:v.tenant,project_id:v.project,review_id:v.review,decision_hash:v.decisionHash,original_run_id:v.originalRun,original_task_id:v.originalTask,run_id:v.run,workflow_id:v.workflow,task_id:v.task,base:structuredClone(v.base)};
 const read=(value:unknown)=>readPerspectiveReplacementScope(value,v.tenant,v.project,v.review,v.originalTask,v.decisionHash);
 const owned=read(wire);wire.base.canvas_hash='f'.repeat(64);expect(owned).toEqual(v);
 expect(()=>read({...wire,unknown:true})).toThrow();expect(()=>read({...wire,review_id:v.project})).toThrow();expect(()=>read({...wire,task_id:v.originalTask})).toThrow();expect(()=>read(null)).toThrow();
});

it('keeps cancelling observable without proposal IO',async()=>{
 const service=api('cancelling');expect(await readPerspectiveReplacementResult(service as unknown as Pick<OwnerAPI,'get'|'perspectiveProposal'>,scope(),new AbortController().signal)).toEqual({state:'pending',workflow_state:'cancelling'});expect(service.perspectiveProposal).not.toHaveBeenCalled();
});
