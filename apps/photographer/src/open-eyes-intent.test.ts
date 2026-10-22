import {describe,expect,it} from 'vitest';
import {verifyOpenEyesRecoverySource,freezeOpenEyesIntent,freezeOpenEyesSelection,readPreparedOpenEyesIntent,readOpenEyesTicket,type OpenEyesIntent,type OpenEyesSelection} from './open-eyes-intent';

const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const binding=(n:number)=>({asset_version_id:id(n),source:ref(n+20),canvas_hash:'b'.repeat(64)});
const selection:OpenEyesSelection={schema_version:1,shoot_session_id:id(1),snapshot_id:id(2),ordinal:0,
 base:binding(3),donor:binding(4),strength:.5,eyes_mask:ref(5),protected:ref(6),
 target_pixel:ref(7),protected_pixel:ref(8),source_faces:ref(9),donor_faces:ref(10),
 source_observation_id:id(11),donor_observation_id:id(12)};
const prepared:OpenEyesIntent={schema_version:1,shoot_session_id:selection.shoot_session_id,
 snapshot_id:selection.snapshot_id,ordinal:0,source_observation_id:selection.source_observation_id,
 donor_observation_id:selection.donor_observation_id,target_pixel:selection.target_pixel,
 protected_pixel:selection.protected_pixel,params_proposal:{schema_version:1,
 base:selection.base,donor:selection.donor,strength:.5,source_face_id:id(13),donor_face_id:id(14),
 eyes_mask:selection.eyes_mask,protected:selection.protected,confirmation:ref(15),
 source_faces:selection.source_faces,donor_faces:selection.donor_faces}};

describe('open eyes client authority boundary',()=>{
 it('retains selected inputs and strict queued receipt',()=>{
  expect(freezeOpenEyesSelection(selection)).toEqual(selection);
  expect(readPreparedOpenEyesIntent(prepared,selection)).toEqual(prepared);
  expect(readOpenEyesTicket({run_id:id(30),workflow_id:id(31),task_id:id(32),state:'queued'}).state).toBe('queued');
 });
 it('rejects changed physical masks and client authority fields',()=>{
  expect(()=>readPreparedOpenEyesIntent({...prepared,target_pixel:ref(99)},selection)).toThrow();
  expect(()=>freezeOpenEyesIntent({...prepared,params_proposal:{...prepared.params_proposal,consent_id:id(33)}} as unknown as OpenEyesIntent)).toThrow();
  expect(()=>freezeOpenEyesSelection({...selection,protected_pixel:selection.target_pixel})).toThrow();
 });
});

it('restores an owned copy only for the exact live source binding',()=>{
 const restored=verifyOpenEyesRecoverySource(prepared,selection.base);
 expect(restored).toEqual(prepared);expect(restored).not.toBe(prepared);
 restored.params_proposal.strength=.2;expect(prepared.params_proposal.strength).toBe(.5);
});
it.each([
 {...selection.base,asset_version_id:id(99)},
 {...selection.base,canvas_hash:'c'.repeat(64)},
 {...selection.base,source:{...selection.base.source,artifact_id:id(99)}},
 {...selection.base,source:{...selection.base.source,sha256:'c'.repeat(64)}},
])('refuses recovery when any live base pin changes',live=>{
 expect(()=>verifyOpenEyesRecoverySource(prepared,live)).toThrow('RECOVERY_SOURCE_CHANGED');
});
