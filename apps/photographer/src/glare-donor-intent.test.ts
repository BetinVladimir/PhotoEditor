import {describe,expect,it} from 'vitest';
import {freezeGlareDonorIntent,freezeGlareDonorPreparation,readPreparedGlareDonorIntent,readGlareDonorTicket,type GlareDonorIntent,type GlareDonorPreparation} from './glare-donor-intent';

const id=(n:number)=>`00000000-0000-4000-8000-${n.toString().padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const intent=():GlareDonorIntent=>({schema_version:1,shoot_session_id:id(1),snapshot_id:id(2),ordinal:0,
 source_observation_id:id(3),donor_observation_id:id(4),params_proposal:{schema_version:1,mode:'donor',
 base:{asset_version_id:id(5),source:ref(6),canvas_hash:'b'.repeat(64)},
 donor:{asset_version_id:id(7),source:ref(8),canvas_hash:'c'.repeat(64)},
 strength:.5,glasses_mask:ref(9),protected:ref(10),confirmation:ref(11),
 source_faces:ref(12),donor_faces:ref(13),model_lock_sha:'d'.repeat(64),calibration:ref(14)}});

describe('donor glare submission boundary',()=>{
 it('keeps preparation selection separate from server qualification and binds the answer',()=>{
  const p=intent(),selection:GlareDonorPreparation={schema_version:1,shoot_session_id:p.shoot_session_id,snapshot_id:p.snapshot_id,ordinal:p.ordinal,base:p.params_proposal.base,donor:p.params_proposal.donor,strength:p.params_proposal.strength,glasses_mask:p.params_proposal.glasses_mask,protected:p.params_proposal.protected,target_pixel:ref(15),protected_pixel:ref(16),source_faces:p.params_proposal.source_faces,donor_faces:p.params_proposal.donor_faces,source_observation_id:p.source_observation_id,donor_observation_id:p.donor_observation_id};
  const frozen=freezeGlareDonorPreparation(selection);
  selection.strength=1;
  expect(frozen.strength).toBe(.5);
  expect(readPreparedGlareDonorIntent(p,frozen).params_proposal.mode).toBe('donor');
  expect(()=>readPreparedGlareDonorIntent(p,selection)).toThrow();
  expect(()=>freezeGlareDonorPreparation({...frozen,qualification:{}} as GlareDonorPreparation)).toThrow();
 });
 it('owns the exact user selection and refuses deployment authority',()=>{
  const request=intent(),frozen=freezeGlareDonorIntent(request);
  request.params_proposal.strength=1;
  expect(frozen.params_proposal.strength).toBe(.5);
  for(const extra of ['qualification','capacity','run_id','workflow_id']){
   expect(()=>freezeGlareDonorIntent({...intent(),[extra]:id(15)} as GlareDonorIntent)).toThrow();
  }
 });
 it('refuses same observation, duplicate inputs and forged receipt',()=>{
  const same=intent();same.donor_observation_id=same.source_observation_id;
  expect(()=>freezeGlareDonorIntent(same)).toThrow();
  const duplicate=intent();duplicate.params_proposal.protected=duplicate.params_proposal.glasses_mask;
  expect(()=>freezeGlareDonorIntent(duplicate)).toThrow();
  expect(readGlareDonorTicket({run_id:id(1),workflow_id:id(2),task_id:id(3),state:'queued'}).task_id).toBe(id(3));
  expect(()=>readGlareDonorTicket({run_id:id(1),workflow_id:id(2),state:'queued'})).toThrow();
 });
});
