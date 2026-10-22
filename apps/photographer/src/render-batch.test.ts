import {it,expect,vi} from 'vitest';
import {OwnerAPI} from './owner-api';
import {renderBatchSubmission,renderBatchFromSnapshots,renderBatchTicket} from './render-batch';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`,sha='a'.repeat(64);
const selection=()=>({source:{asset_version_id:id(1),source:{artifact_id:id(2),sha256:sha},canvas_hash:sha},recipe:{artifact_id:id(3),sha256:sha}});
const request=()=>({input_snapshot_id:id(4),policy_sha:sha,selection:[selection()]});
it('queues owned frozen refs with a stable key and cancellation',async()=>{
 let resolve!:(v:unknown)=>void;const transport=vi.fn((..._args:unknown[])=>new Promise(r=>{resolve=r}));const api=new OwnerAPI({request:transport} as never,'00000000-0000-4000-8000-000000000010'),signal=new AbortController().signal;
 const body=request(),pending=api.queueRenderBatch('00000000-0000-4000-8000-000000000011',body,'same-key',signal);body.selection[0].recipe.sha256='b'.repeat(64);
 expect(transport.mock.calls[0]).toEqual(['/api/v1/tenants/00000000-0000-4000-8000-000000000010/projects/00000000-0000-4000-8000-000000000011/render-batches',expect.objectContaining({body:request(),idempotencyKey:'same-key',signal})]);
 const wire={run_id:id(5),state:'queued',child_run_ids:[id(6)]};resolve(wire);const accepted=await pending;wire.child_run_ids[0]=id(7);expect(accepted.child_run_ids).toEqual([id(6)]);
});
it('retains snapshot order and denies duplicate versions/client runtime',()=>{
 const item=selection(),frozen={document_id:id(8),draft_document_id:id(9),draft_revision:'1',...item};
 expect(renderBatchFromSnapshots(id(4),sha,[frozen]).selection).toEqual([item]);
 expect(()=>renderBatchSubmission({...request(),selection:[item,item]})).toThrow();
 expect(()=>renderBatchSubmission({...request(),runtime:'cuda'} as never)).toThrow();
 expect(()=>renderBatchTicket({run_id:id(5),state:'queued',child_run_ids:[id(6),id(6)]})).toThrow();
});

it('rejects source artifact reused as frozen recipe, matching Go admission',()=>{
 const body=request();body.selection[0].recipe={...body.selection[0].source.source};
 expect(()=>renderBatchSubmission(body)).toThrow('RENDER_BATCH_INVALID');
 const upper=request();upper.selection[0].source.source.artifact_id='aaaaaaaa-1111-1111-1111-111111111111';upper.selection[0].recipe.artifact_id='AAAAAAAA-1111-1111-1111-111111111111';
 expect(()=>renderBatchSubmission(upper)).toThrow('RENDER_BATCH_INVALID');
});
