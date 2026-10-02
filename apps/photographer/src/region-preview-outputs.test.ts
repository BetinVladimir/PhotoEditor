import {describe,it,expect,vi} from 'vitest';
import {waitRegionOutputs,regionPollDelay} from './region-preview-outputs';
import type {RunStatus,RunOutput} from './catalog-types';
const id='11111111-1111-4111-8111-111111111111';
const ticket={run_id:id,workflow_id:id,state:'queued' as const};
const run=(status:string)=>({id,status} as RunStatus);
const outputs=():RunOutput[]=>[{artifact_id:id,role:'document',sha256:'a'.repeat(64),bytes:100,media_type:'application/json',url:'https://photo.test/document',expires_at:new Date(Date.now()+60000).toISOString()},{artifact_id:'22222222-2222-4222-8222-222222222222',role:'derivative',sha256:'b'.repeat(64),bytes:500,media_type:'image/jpeg',url:'https://photo.test/derivative',expires_at:new Date(Date.now()+60000).toISOString()}];
describe('Region output polling',()=>{
 it('waits for completion and returns independent bounded catalog references',async()=>{
  const items=outputs();const reader={run:vi.fn().mockResolvedValueOnce(run('queued')).mockResolvedValueOnce(run('running')).mockResolvedValue(run('completed')),runOutputs:vi.fn().mockResolvedValue({items})};const delay=vi.fn().mockResolvedValue(undefined);const signal=new AbortController().signal;
  const result=await waitRegionOutputs(reader,ticket,signal,delay);expect(delay).toHaveBeenCalledTimes(2);expect(reader.runOutputs).toHaveBeenCalledWith(id,signal);result.derivative.url='changed';expect(items[1].url).not.toBe('changed');
 });
 it('does not load outputs after cancellation during an ignored transport abort',async()=>{
  const controller=new AbortController();const reader={run:vi.fn(async()=>{controller.abort();return run('completed');}),runOutputs:vi.fn()};await expect(waitRegionOutputs(reader,ticket,controller.signal)).rejects.toThrow('REGION_PREVIEW_ABORTED');expect(reader.runOutputs).not.toHaveBeenCalled();
 });
 for(const status of ['failed','cancelled','completed_with_errors','waiting_review'])it(`rejects ${status}`,async()=>{
  const reader={run:vi.fn().mockResolvedValue(run(status)),runOutputs:vi.fn()};await expect(waitRegionOutputs(reader,ticket,new AbortController().signal)).rejects.toThrow('REGION_RUN_FAILED');expect(reader.runOutputs).not.toHaveBeenCalled();
 });
 it('rejects wrong run identity and ambiguous or oversized outputs',async()=>{
  const reader={run:vi.fn().mockResolvedValue({...run('completed'),id:'other'}),runOutputs:vi.fn()};await expect(waitRegionOutputs(reader,ticket,new AbortController().signal)).rejects.toThrow('BINDING');reader.run.mockResolvedValue(run('completed'));
  for(const items of [[],[...outputs(),outputs()[1]],outputs().map(x=>({...x,bytes:16777217})),outputs().map(x=>({...x,expires_at:'2000-01-01'})),outputs().map(x=>({...x,media_type:'image/tiff'}))]){reader.runOutputs.mockResolvedValue({items});await expect(waitRegionOutputs(reader,ticket,new AbortController().signal)).rejects.toThrow('REGION_OUTPUT_INVALID');}
 });
 it('bounds polling and cancels the actual delay',async()=>{
  const reader={run:vi.fn().mockResolvedValue(run('running')),runOutputs:vi.fn()};const delay=vi.fn().mockResolvedValue(undefined);await expect(waitRegionOutputs(reader,ticket,new AbortController().signal,delay)).rejects.toThrow('TIMEOUT');expect(reader.run).toHaveBeenCalledTimes(90);expect(delay).toHaveBeenCalledTimes(89);
  const controller=new AbortController();const pending=regionPollDelay(60000,controller.signal);controller.abort();await expect(pending).rejects.toThrow('ABORTED');
 });
});
