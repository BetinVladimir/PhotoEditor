// @vitest-environment jsdom
import {expectEnabled,clickWhenEnabled} from './test/ui-readiness';
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
const memory=new Map<string,string>();
vi.mock('@react-native-async-storage/async-storage',()=>({default:{
 getItem:async(key:string)=>memory.get(key)??null,
 setItem:async(key:string,value:string)=>{memory.set(key,value);},
 removeItem:async(key:string)=>{memory.delete(key);},
}}));
import {GlareDonorControls} from './glare-donor-controls';
import {APIError} from '@photoeditor/api-client';
import type {OwnerAPI} from './owner-api';
import type {GlareDonorPreparation,GlareDonorIntent} from './glare-donor-intent';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const selection:GlareDonorPreparation={schema_version:1,shoot_session_id:id(1),snapshot_id:id(2),ordinal:0,
 base:{asset_version_id:id(5),source:ref(6),canvas_hash:'b'.repeat(64)},donor:{asset_version_id:id(7),source:ref(8),canvas_hash:'c'.repeat(64)},
 strength:.5,glasses_mask:ref(9),protected:ref(10),target_pixel:ref(15),protected_pixel:ref(16),source_faces:ref(12),donor_faces:ref(13),source_observation_id:id(3),donor_observation_id:id(4)};
const prepared:GlareDonorIntent={schema_version:1,shoot_session_id:selection.shoot_session_id,snapshot_id:selection.snapshot_id,ordinal:0,source_observation_id:selection.source_observation_id,donor_observation_id:selection.donor_observation_id,
 params_proposal:{schema_version:1,mode:'donor',base:selection.base,donor:selection.donor,strength:.5,glasses_mask:selection.glasses_mask,protected:selection.protected,confirmation:ref(11),source_faces:selection.source_faces,donor_faces:selection.donor_faces,model_lock_sha:'d'.repeat(64),calibration:ref(14)}};
afterEach(()=>{cleanup();memory.clear();});
it('uses one preparation key for submission and verifies the accepted workflow',async()=>{
 const ticket={run_id:id(20),workflow_id:id(21),task_id:id(22),state:'queued' as const};
 const prepareGlareDonor=vi.fn(async(_project:string,_body:GlareDonorPreparation,_key:string)=>prepared),submitGlareDonor=vi.fn(async(_project:string,_body:GlareDonorIntent,_key:string)=>ticket);
 const api={prepareGlareDonor,submitGlareDonor,get:vi.fn(async()=>({run_id:ticket.run_id,workflow_id:ticket.workflow_id}))} as unknown as OwnerAPI;
 const onSubmitted=vi.fn();render(<GlareDonorControls api={api} origin="https://photo.test" user={id(30)} tenant={id(31)} project={id(32)} baseVersion={selection.base.asset_version_id} selection={selection} onSubmitted={onSubmitted}/>);
 await waitFor(()=>expectEnabled(screen.getByRole('button',{name:'Prepare and submit'})));
 await clickWhenEnabled(screen.getByRole('button',{name:'Prepare and submit'}));
 await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(ticket));
 expect(prepareGlareDonor).toHaveBeenCalledOnce();expect(submitGlareDonor).toHaveBeenCalledOnce();
 expect(prepareGlareDonor.mock.calls[0][2]).toBe(submitGlareDonor.mock.calls[0][2]);
});

it.each(['prepare','submit','workflow'])('withdraws glare authority after %s denial while preserving an existing request',async(stage)=>{
 const ticket={run_id:id(20),workflow_id:id(21),task_id:id(22),state:'queued' as const};
 const prepareGlareDonor=vi.fn().mockResolvedValue(prepared),submitGlareDonor=vi.fn().mockResolvedValue(ticket),get=vi.fn().mockResolvedValue({run_id:ticket.run_id,workflow_id:ticket.workflow_id});
 const denied=new APIError(403,'FORBIDDEN');
 if(stage==='prepare')prepareGlareDonor.mockRejectedValue(denied);else if(stage==='submit')submitGlareDonor.mockRejectedValue(denied);else get.mockRejectedValue(denied);
 const onSubmitted=vi.fn();render(<GlareDonorControls api={{prepareGlareDonor,submitGlareDonor,get} as unknown as OwnerAPI} origin="https://glare-denial.test" user={id(30)} tenant={id(31)} project={id(32)} baseVersion={selection.base.asset_version_id} selection={selection} onSubmitted={onSubmitted}/>);
 await waitFor(()=>expectEnabled(screen.getByRole('button',{name:'Prepare and submit'})));
 await clickWhenEnabled(screen.getByRole('button',{name:'Prepare and submit'}));await screen.findByText(/Donor glare access is no longer available/);
 expect(screen.getByRole('button',{name:'Prepare and submit'}).getAttribute('aria-disabled')).toBe('true');expect(onSubmitted).not.toHaveBeenCalled();
 const saved=[...memory.values()].map(value=>JSON.parse(value)).filter(value=>value.schema_version===1&&value.project===id(32));
 if(stage==='prepare'){expect(saved).toHaveLength(0);expect(submitGlareDonor).not.toHaveBeenCalled()}else{expect(saved).toHaveLength(1);expect(saved[0].body).toEqual(prepared);expect(saved[0].ticket).toEqual(stage==='workflow'?ticket:null)}
});
