import {describe,it,expect} from 'vitest';
import {freezePerspectiveLinesSubmission,readPerspectiveLinesSubmissionReceipt,type PerspectiveLinesSubmissionIntent} from './perspective-lines-submission';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const intent=():PerspectiveLinesSubmissionIntent=>({schema_version:1,snapshot_id:id(3),base:{asset_version_id:id(4),source:{artifact_id:id(5),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)}});
describe('perspective detector submission',()=>{
 it('owns the source-bound request',()=>{const value=intent();const body=freezePerspectiveLinesSubmission(id(1),id(2),value);value.base.source.sha256='c'.repeat(64);expect(body.base.source.sha256).toBe('a'.repeat(64));});
 it('rejects client-owned execution fields and invalid scope',()=>{for(const key of ['runtime_lock_sha','profile','task_id','source_jcs']){expect(()=>freezePerspectiveLinesSubmission(id(1),id(2),{...intent(),[key]:'injected'} as PerspectiveLinesSubmissionIntent)).toThrow();}expect(()=>freezePerspectiveLinesSubmission('invalid',id(2),intent())).toThrow();});
 it('reads only admitted receipts and owns the response',()=>{for(const state of ['queued','running','waiting_review','completed']){const value={run_id:id(6),workflow_id:id(7),task_id:id(8),state};const receipt=readPerspectiveLinesSubmissionReceipt(value);value.task_id=id(9);expect(receipt.task_id).toBe(id(8));}});
 it('rejects draft, forged fields and aliases',()=>{for(const state of ['draft','sealed','failed','cancelled'])expect(()=>readPerspectiveLinesSubmissionReceipt({run_id:id(6),workflow_id:id(7),task_id:id(8),state})).toThrow();expect(()=>readPerspectiveLinesSubmissionReceipt({run_id:id(6),workflow_id:id(7),task_id:id(7),state:'queued'})).toThrow();expect(()=>readPerspectiveLinesSubmissionReceipt({run_id:id(6),workflow_id:id(7),task_id:id(8),state:'queued',extra:true})).toThrow();});
});
