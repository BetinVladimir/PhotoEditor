// @vitest-environment jsdom
import React from 'react';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
vi.mock('react-native',async()=>await import('react-native-web'));
import {AppState} from 'react-native';
import {RelightAnalysisProgress,readRelightAnalysisWorkflow} from './relight-analysis-progress';
import type {OwnerAPI} from './owner-api';
import type {RegionJobTicket} from './region-preview';
const run='11111111-1111-4111-8111-111111111111',workflow='22222222-2222-4222-8222-222222222222';
const ticket={run_id:run,workflow_id:workflow,state:'queued'} as RegionJobTicket;
const labels={queued:'Queued',running:'Running',waiting_review:'Review',cancelling:'Stopping',cancelled:'Cancelled',completed:'Completed',failed:'Failed',unavailable:'Unavailable',refresh:'Refresh results'};
afterEach(cleanup);
it('binds status to original workflow and monotonic revision',()=>{
 const first=readRelightAnalysisWorkflow({run_id:run,workflow_id:workflow,revision:'1',state:'running'},ticket,null);
 expect(first.state).toBe('running');
 for(const patch of [{run_id:workflow},{workflow_id:run},{revision:'0'},{revision:'9223372036854775808'},{state:'unknown'}])
  expect(()=>readRelightAnalysisWorkflow({...first,...patch},ticket,first)).toThrow();
 expect(()=>readRelightAnalysisWorkflow({...first,revision:'2',state:'completed'},ticket,first).state).toBe('completed');
 const done=readRelightAnalysisWorkflow({...first,revision:'2',state:'completed'},ticket,first);
 expect(()=>readRelightAnalysisWorkflow({...done,revision:'3',state:'running'},ticket,done)).toThrow('RELIGHT_ANALYSIS_STATUS_REGRESSED');
 expect(()=>readRelightAnalysisWorkflow({...done,revision:'1'},ticket,done)).toThrow('RELIGHT_ANALYSIS_STATUS_REGRESSED');
});
it('offers fresh result discovery only after authenticated completion',async()=>{
 const get=vi.fn().mockResolvedValue({run_id:run,workflow_id:workflow,revision:'2',state:'completed',outputs:[],error_code:null}),onRefresh=vi.fn();
 render(<RelightAnalysisProgress api={{get} as unknown as OwnerAPI} ticket={ticket} labels={labels} onRefresh={onRefresh}/>);
 await screen.findByText('Completed');fireEvent.click(screen.getByText('Refresh results'));
 expect(onRefresh).toHaveBeenCalledOnce();
 expect(get.mock.calls[0][0]).toContain(workflow);
});
it('rejects a foreign workflow without offering refresh',async()=>{
 const get=vi.fn().mockResolvedValue({run_id:run,workflow_id:run,revision:'2',state:'completed'}),onRefresh=vi.fn();
 render(<RelightAnalysisProgress api={{get} as unknown as OwnerAPI} ticket={ticket} labels={labels} onRefresh={onRefresh}/>);
 await waitFor(()=>expect(screen.getByRole('alert').textContent).toBe('Unavailable'));
 expect(screen.queryByText('Completed')).toBeNull();expect(onRefresh).not.toHaveBeenCalled();
});

it('does not start a second poll loop for repeated active notifications',async()=>{
 let notify:(state:'active'|'background')=>void=()=>{};
 const listener=vi.spyOn(AppState,'addEventListener').mockImplementation((_event,callback)=>{
  notify=callback as typeof notify;
  return {remove:()=>{}};
 });
 try{
  const get=vi.fn().mockResolvedValue({run_id:run,workflow_id:workflow,revision:'1',state:'running'});
  render(<RelightAnalysisProgress api={{get} as unknown as OwnerAPI} ticket={ticket} labels={labels} onRefresh={()=>{}}/>);
  await screen.findByText('Running');
  await act(async()=>{notify('active');notify('active');});
  expect(get).toHaveBeenCalledTimes(1);
  await act(async()=>{notify('background');notify('active');});
  await waitFor(()=>expect(get).toHaveBeenCalledTimes(2));
 }finally{listener.mockRestore();}
});
