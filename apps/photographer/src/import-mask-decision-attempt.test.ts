import {it,expect} from 'vitest';
import {ImportMaskDecisionAttempt} from './import-mask-decision-attempt';
const id='11111111-1111-4111-8111-111111111111';
const request=()=>({action:'revoke' as const,expected_epoch:1,entry_id:id,target_version_id:id,override_artifact_id:id,mask_artifact_id:id});
it('owns the unresolved epoch and permits only an exact replay',()=>{
 const attempt=new ImportMaskDecisionAttempt(),input=request();attempt.prepare(input);input.expected_epoch=2;
 expect(attempt.snapshot()?.expected_epoch).toBe(1);expect(()=>attempt.prepare(input)).toThrow('INTERCHANGE_MASK_DECISION_UNRESOLVED');
 const replay=attempt.prepare(request());replay.expected_epoch=9;expect(attempt.snapshot()?.expected_epoch).toBe(1);
 expect(()=>attempt.acknowledge(input)).toThrow();attempt.acknowledge(request());expect(attempt.snapshot()).toBeUndefined();expect(attempt.prepare(input).expected_epoch).toBe(2);
});
