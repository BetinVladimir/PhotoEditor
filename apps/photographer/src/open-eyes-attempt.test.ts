import {describe,expect,it,vi} from 'vitest';
import {OpenEyesAttempt} from './open-eyes-attempt';
import type {OpenEyesIntent,OpenEyesTicket} from './open-eyes-intent';

const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const body:OpenEyesIntent={schema_version:1,shoot_session_id:id(1),snapshot_id:id(2),ordinal:0,
 source_observation_id:id(3),donor_observation_id:id(4),target_pixel:ref(5),protected_pixel:ref(6),
 params_proposal:{schema_version:1,base:{asset_version_id:id(7),source:ref(8),canvas_hash:'b'.repeat(64)},
  donor:{asset_version_id:id(9),source:ref(10),canvas_hash:'b'.repeat(64)},strength:.5,
  source_face_id:id(11),donor_face_id:id(12),eyes_mask:ref(13),protected:ref(14),
  confirmation:ref(15),source_faces:ref(16),donor_faces:ref(17)}};
const ticket:OpenEyesTicket={run_id:id(30),workflow_id:id(31),task_id:id(32),state:'queued'};

describe('open eyes durable attempt',()=>{
 it('persists exact body before send and preserves it after ambiguous response',async()=>{
  const writes:unknown[]=[];const attempt=new OpenEyesAttempt(id(20),'eyes-key',body,{write:async record=>{writes.push(record);}});
  const fail=vi.fn(async()=>{throw new Error('connection lost');});
  await expect(attempt.submit(fail)).rejects.toThrow('connection lost');
  expect(writes).toHaveLength(1);expect(attempt.state).toBe('unknown');
  const send=vi.fn(async(_project:string,sent:OpenEyesIntent,key:string)=>{
   expect(sent).toEqual(body);expect(key).toBe('eyes-key');return ticket;
  });
  await expect(attempt.submit(send)).resolves.toEqual(ticket);
  expect(writes).toHaveLength(3);expect(attempt.state).toBe('accepted');
  await expect(attempt.submit(send)).resolves.toEqual(ticket);
  expect(send).toHaveBeenCalledOnce();
  const cancelled=new AbortController();cancelled.abort();
  await expect(attempt.submit(send,cancelled.signal)).rejects.toThrow('OPEN_EYES_ATTEMPT_CANCELLED');expect(send).toHaveBeenCalledOnce();
 });
});
it('keeps the accepted open eyes ticket after caller cancellation',async()=>{
 const signal=new AbortController(),writes:unknown[]=[];
 const attempt=new OpenEyesAttempt(id(20),'eyes-key',body,{write:async record=>{writes.push(record);}});
 await expect(attempt.submit(async()=>{signal.abort();return ticket;},signal.signal)).rejects.toThrow('OPEN_EYES_ATTEMPT_CANCELLED');
 expect(attempt.state).toBe('accepted');expect((writes.at(-1) as {ticket:unknown}).ticket).toEqual(ticket);
 await expect(attempt.submit(async()=>{throw new Error('resubmitted');})).resolves.toEqual(ticket);
});
