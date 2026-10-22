import {expect,it,vi} from 'vitest';
import {freezeRelightIntent,type RelightIntent} from './relight-intent';
import {RelightAttempt} from './relight-attempt';
const id='11111111-1111-4111-8111-111111111111',other='22222222-2222-4222-8222-222222222222';
const ref=()=>({artifact_id:id,sha256:'a'.repeat(64)});
function intent():RelightIntent{return {schema_version:1,snapshot_id:id,ordinal:0,subject_kind:'manual',params:{schema_version:1,base:{asset_version_id:id,source:ref(),canvas_hash:'b'.repeat(64)},strength:.5,depth:ref(),subject:ref(),protected:ref(),calibration:ref(),lights:[{x:.5,y:.5,radius:.3,ev:.5}]}};}
const ticket={run_id:id,workflow_id:other,state:'queued' as const};
it('owns nested authored controls and rejects authority injection and invalid bounds',()=>{
 const original=intent(),copy=freezeRelightIntent(original);original.params.lights[0].ev=1;
 expect(copy.params.lights[0].ev).toBe(.5);
 for(const field of ['run_id','workflow_id','pin','tenant_id','capacity'])expect(()=>freezeRelightIntent({...intent(),[field]:id} as RelightIntent)).toThrow();
 for(const fault of ['strength','radius','ordinal','light_count','nested_field']){
 const value=intent();
 if(fault==='strength')value.params.strength=NaN;
 if(fault==='radius')value.params.lights[0].radius=.01;
 if(fault==='ordinal')value.ordinal=5000;
 if(fault==='light_count')value.params.lights=[];
 if(fault==='nested_field')Object.assign(value.params,{runtime_sha:'c'.repeat(64)});
 expect(()=>freezeRelightIntent(value)).toThrow();
 }
});
it('persists exact request before send and retries unknown result with same key/body',async()=>{
 const write=vi.fn().mockResolvedValue(undefined),send=vi.fn().mockRejectedValueOnce(new Error('network')).mockResolvedValue(ticket);
 const original=intent(),attempt=new RelightAttempt(id,'same-key',original,{write});original.params.strength=1;
 await expect(attempt.submit(send)).rejects.toThrow('network');expect(attempt.state).toBe('unknown');
 expect(write.mock.invocationCallOrder[0]).toBeLessThan(send.mock.invocationCallOrder[0]);
 await expect(attempt.submit(send)).resolves.toEqual(ticket);
 expect(send.mock.calls[0][1]).toEqual(send.mock.calls[1][1]);expect(send.mock.calls[1][2]).toBe('same-key');
 expect(send.mock.calls[0][1].params.strength).toBe(.5);
 await attempt.submit(send);expect(send).toHaveBeenCalledTimes(2);
 const cancelled=new AbortController();cancelled.abort();
 await expect(attempt.submit(send,cancelled.signal)).rejects.toThrow('RELIGHT_ATTEMPT_CANCELLED');expect(send).toHaveBeenCalledTimes(2);
});
it('does not send if journal fails and restores an ambiguous attempt',async()=>{
 const send=vi.fn(),attempt=new RelightAttempt(id,'key',intent(),{write:vi.fn().mockRejectedValue(new Error('storage'))});
 await expect(attempt.submit(send)).rejects.toThrow('storage');expect(send).not.toHaveBeenCalled();
 const restored=RelightAttempt.restore(attempt.snapshot,{write:vi.fn().mockResolvedValue(undefined)});
 expect(restored.state).toBe('unknown');
 const success=vi.fn().mockResolvedValue(ticket);await restored.submit(success);expect(success.mock.calls[0][2]).toBe('key');
});
it('retries acknowledgement after ticket persistence failure',async()=>{
 const write=vi.fn().mockResolvedValueOnce(undefined).mockRejectedValueOnce(new Error('storage')).mockResolvedValue(undefined);
 const send=vi.fn().mockResolvedValue(ticket),attempt=new RelightAttempt(id,'key',intent(),{write});
 await expect(attempt.submit(send)).rejects.toThrow('storage');expect(attempt.snapshot.ticket).toEqual(ticket);
 await attempt.submit(send);expect(send).toHaveBeenCalledTimes(1);expect(attempt.state).toBe('accepted');
});
it('keeps the accepted relight ticket after caller cancellation',async()=>{
 const signal=new AbortController(),writes:unknown[]=[];
 const attempt=new RelightAttempt(id,'key',intent(),{write:async record=>{writes.push(record);}});
 await expect(attempt.submit(async()=>{signal.abort();return ticket;},signal.signal)).rejects.toThrow('RELIGHT_ATTEMPT_CANCELLED');
 expect(attempt.state).toBe('accepted');expect((writes.at(-1) as {ticket:unknown}).ticket).toEqual(ticket);
 await expect(attempt.submit(async()=>{throw new Error('resubmitted');})).resolves.toEqual(ticket);
});
