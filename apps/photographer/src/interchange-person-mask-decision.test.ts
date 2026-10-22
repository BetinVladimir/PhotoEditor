import {describe,expect,it} from 'vitest';
import {freezeImportPersonMaskDecision,readImportPersonMaskDecision} from './interchange-person-mask-decision';

const id=(n:number)=>`${n.toString(16).padStart(8,'0')}-0000-4000-8000-000000000001`;

describe('imported person-mask decision',()=>{
  const revoke={expected_epoch:1,action:'revoke' as const,entry_id:id(1),target_version_id:id(2),override_artifact_id:id(3),mask_artifact_id:id(4)};
  it('owns a scoped revocation request and requires an exact receipt',()=>{
    const body=freezeImportPersonMaskDecision(revoke);
    expect(body).toEqual(revoke);
    expect(readImportPersonMaskDecision({workflow_id:id(5),entry_id:id(1),status:'revoked',epoch:2},id(5),body).epoch).toBe(2);
    for(const invalid of [{workflow_id:id(6),entry_id:id(1),status:'revoked',epoch:2},{workflow_id:id(5),entry_id:id(1),status:'granted',epoch:2},{workflow_id:id(5),entry_id:id(1),status:'revoked',epoch:0,extra:true}])
      expect(()=>readImportPersonMaskDecision(invalid,id(5),body)).toThrow('INTERCHANGE_MASK_DECISION_RECEIPT_INVALID');
  });
  it('enforces server UTF-8 byte limits for non-ASCII grant documents',()=>{
    const grant={...revoke,action:'grant' as const,recipe_artifact_id:id(7),parsed_jcs:'{}',recipe_jcs:'{}',override_jcs:'{}',mask_jcs:'{}'};
    expect(freezeImportPersonMaskDecision(grant)).toEqual(grant);
    const oversized='"'+'я'.repeat(1<<19)+'"';
    expect(oversized.length).toBeLessThan(1<<20);
    for(const field of ['recipe_jcs','mask_jcs'])expect(()=>freezeImportPersonMaskDecision({...grant,[field]:oversized})).toThrow('INTERCHANGE_MASK_DECISION_INVALID');
  });
  it('refuses archive claims on revoke and incomplete grant evidence',()=>{
    expect(()=>freezeImportPersonMaskDecision({...revoke,parsed_jcs:'{}'})).toThrow('INTERCHANGE_MASK_DECISION_INVALID');
    expect(()=>freezeImportPersonMaskDecision({...revoke,action:'grant',recipe_artifact_id:id(7)})).toThrow('INTERCHANGE_MASK_DECISION_INVALID');
  });
});

it('requires explicit writable epoch and exact successor receipt',()=>{
 const request={action:'revoke' as const,entry_id:id(1),target_version_id:id(2),override_artifact_id:id(3),mask_artifact_id:id(4)};
 for(const expected_epoch of [undefined,-1,1.5,Number.MAX_SAFE_INTEGER])expect(()=>freezeImportPersonMaskDecision({...request,expected_epoch})).toThrow();
 for(const epoch of [1,3])expect(()=>readImportPersonMaskDecision({workflow_id:id(5),entry_id:id(1),status:'revoked',epoch},id(5),{...request,expected_epoch:1})).toThrow();
});
