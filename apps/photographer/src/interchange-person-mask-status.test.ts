import {describe,expect,it} from 'vitest';
import {readImportPersonMaskStatuses} from './interchange-person-mask-status';

const workflow='11111111-1111-4111-8111-111111111111';
const item={entry_id:'22222222-2222-4222-8222-222222222222',override_artifact_id:'33333333-3333-4333-8333-333333333333',mask_artifact_id:'44444444-4444-4444-8444-444444444444',target_version_id:'55555555-5555-4555-8555-555555555555',status:'granted',epoch:1};
describe('persisted imported person mask decisions',()=>{
  it('reads a scoped grant for reopening',()=>expect(readImportPersonMaskStatuses({workflow_id:workflow,items:[item]},workflow)).toEqual([item]));
  it('rejects a foreign workflow',()=>expect(()=>readImportPersonMaskStatuses({workflow_id:'66666666-6666-4666-8666-666666666666',items:[item]},workflow)).toThrow());
  it('rejects duplicate and malformed decisions',()=>{
    expect(()=>readImportPersonMaskStatuses({workflow_id:workflow,items:[item,item]},workflow)).toThrow();
    expect(()=>readImportPersonMaskStatuses({workflow_id:workflow,items:[{...item,status:'pending'}]},workflow)).toThrow();
  });
});
