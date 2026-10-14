import {describe,expect,it} from 'vitest';
import {freezeImportPersonMaskDecision,readImportPersonMaskDecision} from './interchange-person-mask-decision';

const id=(n:number)=>`${n.toString(16).padStart(8,'0')}-0000-4000-8000-000000000001`;

describe('imported person-mask decision',()=>{
  const revoke={action:'revoke' as const,entry_id:id(1),target_version_id:id(2),override_artifact_id:id(3),mask_artifact_id:id(4)};
  it('owns a scoped revocation request and requires an exact receipt',()=>{
    const body=freezeImportPersonMaskDecision(revoke);
    expect(body).toEqual(revoke);
    expect(readImportPersonMaskDecision({workflow_id:id(5),entry_id:id(1),status:'revoked',epoch:2},id(5),body).epoch).toBe(2);
    for(const invalid of [{workflow_id:id(6),entry_id:id(1),status:'revoked',epoch:2},{workflow_id:id(5),entry_id:id(1),status:'granted',epoch:2},{workflow_id:id(5),entry_id:id(1),status:'revoked',epoch:0,extra:true}])
      expect(()=>readImportPersonMaskDecision(invalid,id(5),body)).toThrow('INTERCHANGE_MASK_DECISION_RECEIPT_INVALID');
  });
  it('refuses archive claims on revoke and incomplete grant evidence',()=>{
    expect(()=>freezeImportPersonMaskDecision({...revoke,parsed_jcs:'{}'})).toThrow('INTERCHANGE_MASK_DECISION_INVALID');
    expect(()=>freezeImportPersonMaskDecision({...revoke,action:'grant',recipe_artifact_id:id(7)})).toThrow('INTERCHANGE_MASK_DECISION_INVALID');
  });
});
