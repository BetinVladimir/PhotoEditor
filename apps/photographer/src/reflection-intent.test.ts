import {describe,expect,it} from 'vitest';
import {freezeReflectionPreparation,freezeReflectionIntent,readPreparedReflectionIntent,readReflectionTicket,verifyReflectionRecoverySource,type ReflectionPreparation,type ReflectionIntent} from './reflection-intent';
const id=(n:number)=>`00000000-0000-4000-8000-${n.toString(16).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});

describe('reflection client authority boundary',()=>{
 const selection=():ReflectionPreparation=>({schema_version:1,snapshot_id:id(1),ordinal:0,
  base:{asset_version_id:id(2),source:ref(3),canvas_hash:'b'.repeat(64)},strength:.5,
  mask:ref(4),protected:ref(5),target_pixel:ref(6),protected_pixel:ref(7)});
 const prepared=():ReflectionIntent=>({schema_version:1,snapshot_id:id(1),ordinal:0,
  params_proposal:{schema_version:1,base:selection().base,strength:.5,mask:ref(4),protected:ref(5),
   model_lock_sha:'c'.repeat(64),calibration:ref(8)},target_pixel:ref(6),protected_pixel:ref(7)});
 it('freezes only selected input refs and checks server-owned pins',()=>{
  expect(freezeReflectionPreparation(selection())).toEqual(selection());
  expect(readPreparedReflectionIntent(prepared(),selection())).toEqual(prepared());
  expect(()=>freezeReflectionPreparation({...selection(),qualification:{}} as ReflectionPreparation)).toThrow();
  expect(()=>freezeReflectionIntent({...prepared(),target_pixel:ref(7)})).toThrow();
  expect(()=>readPreparedReflectionIntent({...prepared(),protected_pixel:ref(9)},selection())).toThrow();
  expect(()=>freezeReflectionIntent({...prepared(),params_proposal:{...prepared().params_proposal,model_lock_sha:'invalid'}})).toThrow();
  expect(verifyReflectionRecoverySource(prepared(),selection().base)).toEqual(prepared());
  expect(()=>verifyReflectionRecoverySource(prepared(),{...selection().base,source:ref(9)})).toThrow();
 });
 it('does not treat an expanded or foreign receipt as admission',()=>{
  const ticket={run_id:id(10),workflow_id:id(11),task_id:id(12),state:'queued' as const};
  expect(readReflectionTicket(ticket)).toEqual(ticket);
  expect(()=>readReflectionTicket({...ticket,scope:'other'})).toThrow();
  expect(()=>readReflectionTicket({...ticket,state:'completed'})).toThrow();
 });
});
