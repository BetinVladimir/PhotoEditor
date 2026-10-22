import {it,expect} from 'vitest';
import {importMaskDecisionEpoch} from './import-mask-decision-admission';
const id='11111111-1111-4111-8111-111111111111';
const source={action:'grant' as const,entry_id:id,override_artifact_id:id,mask_artifact_id:id,target_version_id:id};
it('admits only actual transitions at the exact known epoch',()=>{
 expect(()=>importMaskDecisionEpoch(null,source,'grant')).toThrow();expect(importMaskDecisionEpoch([],source,'grant')).toBe(0);expect(importMaskDecisionEpoch([],source,'revoke')).toBeNull();
 const status={...source,status:'granted' as const,epoch:3};
 expect(importMaskDecisionEpoch([status],source,'grant')).toBeNull();expect(importMaskDecisionEpoch([status],source,'revoke')).toBe(3);
 expect(importMaskDecisionEpoch([{...status,status:'revoked'}],source,'grant')).toBe(3);
 expect(()=>importMaskDecisionEpoch([{...status,epoch:Number.MAX_SAFE_INTEGER}],source,'revoke')).toThrow();
});
