import {describe,expect,it} from 'vitest';
import {readImportPersonMaskCandidates,readImportPersonMaskEntries} from './interchange-person-mask-candidates';

const id=(n:number)=>`${n.toString(16).padStart(8,'0')}-0000-4000-8000-000000000001`;
const sha='a'.repeat(64);

describe('import person-mask candidates',()=>{
  const workflow=id(1),entry=id(2);
  const decision={action:'grant',entry_id:entry,target_version_id:id(3),override_artifact_id:id(4),mask_artifact_id:id(5),parsed_jcs:'{}',recipe_artifact_id:id(6),recipe_jcs:'{}',override_jcs:'{}',mask_jcs:JSON.stringify({selector:'person',object_id:null,model_lock_id:sha,artifact:{artifact_id:id(5),sha256:sha}})};
  it('owns an exact workflow and entry response',()=>{
    const source={workflow_id:workflow,entry_id:entry,candidates:[{external_id:'frame-1',decision}]};
    const page=readImportPersonMaskCandidates(source,workflow,entry);
    expect(page.candidates[0].decision).toEqual(decision);
    decision.mask_jcs='changed';expect(page.candidates[0].decision.mask_jcs).not.toBe('changed');
  });
  it('rejects foreign scope, duplicate candidates and unbound mask pixels',()=>{
    const valid={...decision,mask_jcs:JSON.stringify({selector:'person',object_id:null,model_lock_id:sha,artifact:{artifact_id:id(5),sha256:sha}})};
    const candidate={external_id:'frame-1',decision:valid};
    for(const invalid of [
      {workflow_id:id(9),entry_id:entry,candidates:[candidate]},
      {workflow_id:workflow,entry_id:entry,candidates:[candidate,candidate]},
      {workflow_id:workflow,entry_id:entry,candidates:[{...candidate,decision:{...valid,mask_artifact_id:id(8)}}]},
    ])expect(()=>readImportPersonMaskCandidates(invalid,workflow,entry)).toThrow('INTERCHANGE_MASK_CANDIDATES_INVALID');
  });
  it('reads only scoped, unique adopted entry identifiers',()=>{
    const one={entry_id:entry,target_version_id:id(3),asset_id:id(10),external_id:'frame-1'};
    expect(readImportPersonMaskEntries({workflow_id:workflow,entries:[one]},workflow).entries).toEqual([one]);
    for(const invalid of [
      {workflow_id:id(9),entries:[one]},
      {workflow_id:workflow,entries:[one,one]},
      {workflow_id:workflow,entries:[{...one,target_version_id:'invalid'}]},
    ])expect(()=>readImportPersonMaskEntries(invalid,workflow)).toThrow('INTERCHANGE_MASK_ENTRIES_INVALID');
  });
});
