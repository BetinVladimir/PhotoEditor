// @vitest-environment jsdom
import React from 'react';
import {clickWhenEnabled,expectEnabled} from './test/ui-readiness';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
vi.mock('react-native',async()=>await import('react-native-web'));
import {RelightAnalysisAction} from './relight-analysis-action';
import type {OwnerAPI} from './owner-api';
import type {RelightAnalysisAttemptStore} from './relight-analysis-attempt-store';
import type {RelightAnalysisIntent} from './relight-analysis';
const id='11111111-1111-4111-8111-111111111111',other='22222222-2222-4222-8222-222222222222';
const intent:RelightAnalysisIntent={schema_version:1,snapshot_id:id,operation:'depth.predict',base:{asset_version_id:id,source:{artifact_id:id,sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)}};
const labels={create:'Prepare',retry:'Retry',open:'Open',loading:'Loading',unknown:'Unknown',unavailable:'Unavailable'};
afterEach(cleanup);
it('restores original snapshot/key and verifies workflow before navigation',async()=>{
 const submitRelightAnalysis=vi.fn().mockResolvedValue({run_id:id,workflow_id:other,state:'queued'}),verify=vi.fn().mockResolvedValue(undefined),onSubmitted=vi.fn();
 const store={read:vi.fn().mockResolvedValue({schema_version:1,project:id,key:'original-key',body:intent,ticket:null}),write:vi.fn().mockResolvedValue(undefined)} as unknown as RelightAnalysisAttemptStore;
 render(<RelightAnalysisAction api={{submitRelightAnalysis} as unknown as OwnerAPI} project={id} intent={{...intent,snapshot_id:other}} store={store} available={false} labels={labels} verifyTicket={verify} onSubmitted={onSubmitted}/>);
 await screen.findByText('Retry');await clickWhenEnabled(await screen.findByRole('button',{name:'Retry'}));
 await waitFor(()=>expect(onSubmitted).toHaveBeenCalledOnce());
 expect(submitRelightAnalysis.mock.calls[0][1]).toEqual(intent);expect(submitRelightAnalysis.mock.calls[0][2]).toBe('original-key');
 expect(verify.mock.invocationCallOrder[0]).toBeLessThan(onSubmitted.mock.invocationCallOrder[0]);
});
it('journal read failure blocks new admission',async()=>{
 const submitRelightAnalysis=vi.fn(),store={read:vi.fn().mockRejectedValue(new Error('corrupt journal'))} as unknown as RelightAnalysisAttemptStore;
 render(<RelightAnalysisAction api={{submitRelightAnalysis} as unknown as OwnerAPI} project={id} intent={intent} store={store} available={false} labels={labels} verifyTicket={async()=>{}} onSubmitted={()=>{}}/>);
 await screen.findByText('Unavailable');fireEvent.click(screen.getByText('Prepare'));expect(submitRelightAnalysis).not.toHaveBeenCalled();
});

it('resets a failed journal when the authenticated storage scope changes',async()=>{
 const failed={read:vi.fn().mockRejectedValue(new Error('old scope unavailable'))} as unknown as RelightAnalysisAttemptStore;
 const next={read:vi.fn().mockResolvedValue(null),write:vi.fn().mockResolvedValue(undefined)} as unknown as RelightAnalysisAttemptStore;
 const api={submitRelightAnalysis:vi.fn()} as unknown as OwnerAPI;
 const shared={api,project:id,intent,available:false,labels,verifyTicket:async()=>{},onSubmitted:()=>{}};
 const view=render(<RelightAnalysisAction {...shared} store={failed}/>);
 await screen.findByText('Unavailable');
 view.rerender(<RelightAnalysisAction {...shared} store={next}/>);
 await waitFor(()=>expect(screen.queryByText('Unavailable')).toBeNull());
 await waitFor(()=>expectEnabled(screen.getByRole('button',{name:'Prepare'})));
 expect(next.read).toHaveBeenCalledOnce();
});
it('does not navigate or verify an old response after the journal scope changes',async()=>{
 let resolve!:(value:unknown)=>void;
 const pending=new Promise(value=>{resolve=value;});
 const api={submitRelightAnalysis:vi.fn().mockReturnValue(pending)} as unknown as OwnerAPI;
 const old={read:vi.fn().mockResolvedValue({schema_version:1,project:id,key:'old-key',body:intent,ticket:null}),write:vi.fn().mockResolvedValue(undefined)} as unknown as RelightAnalysisAttemptStore;
 const next={read:vi.fn().mockResolvedValue(null),write:vi.fn().mockResolvedValue(undefined)} as unknown as RelightAnalysisAttemptStore;
 const verify=vi.fn(),navigate=vi.fn();
 const shared={api,project:id,intent,available:false,labels,verifyTicket:verify,onSubmitted:navigate};
 const view=render(<RelightAnalysisAction {...shared} store={old}/>);
 await screen.findByText('Retry');await clickWhenEnabled(await screen.findByRole('button',{name:'Retry'}));
 await waitFor(()=>expect(api.submitRelightAnalysis).toHaveBeenCalledOnce());
 view.rerender(<RelightAnalysisAction {...shared} store={next}/>);
 await screen.findByText('Prepare');
 resolve({run_id:id,workflow_id:other,state:'queued'});
 await pending;
 expect(verify).not.toHaveBeenCalled();expect(navigate).not.toHaveBeenCalled();
});

it('opens accepted analysis without worker capability after authoritative verification',async()=>{
 const ticket={run_id:id,workflow_id:other,state:'queued'},submitRelightAnalysis=vi.fn(),verify=vi.fn().mockResolvedValue(undefined),onSubmitted=vi.fn();
 const store={read:vi.fn().mockResolvedValue({schema_version:1,project:id,key:'saved-key',body:intent,ticket}),write:vi.fn()} as unknown as RelightAnalysisAttemptStore;
 render(<RelightAnalysisAction api={{submitRelightAnalysis} as unknown as OwnerAPI} project={id} intent={intent} store={store} available={false} capabilityAvailable={false} labels={labels} verifyTicket={verify} onSubmitted={onSubmitted}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'Open'}));await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(ticket));expect(verify).toHaveBeenCalledOnce();expect(submitRelightAnalysis).not.toHaveBeenCalled();expect(store.write).not.toHaveBeenCalled();
});
it('recovers unknown receipt without worker using replay-only and original body/key',async()=>{
 const ticket={run_id:id,workflow_id:other,state:'queued'},submitRelightAnalysis=vi.fn().mockResolvedValue(ticket),verify=vi.fn().mockResolvedValue(undefined),onSubmitted=vi.fn();
 const saved={schema_version:1,project:id,key:'saved-key',body:intent,ticket:null},store={read:vi.fn().mockResolvedValue(saved),write:vi.fn().mockResolvedValue(undefined)} as unknown as RelightAnalysisAttemptStore;
 render(<RelightAnalysisAction api={{submitRelightAnalysis} as unknown as OwnerAPI} project={id} intent={intent} store={store} available={false} labels={labels} capabilityAvailable={false} verifyTicket={verify} onSubmitted={onSubmitted}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'Retry'}));
 await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(ticket));
 expect(submitRelightAnalysis).toHaveBeenCalledOnce();expect(submitRelightAnalysis.mock.calls[0].slice(1,3)).toEqual([saved.body,saved.key]);expect(submitRelightAnalysis.mock.calls[0][4]).toBe(true);
 expect(verify.mock.invocationCallOrder[0]).toBeLessThan(onSubmitted.mock.invocationCallOrder[0]);
});
it('checks current analysis capability after journal IO and retries the same request',async()=>{
 let finish!:()=>void;const write=vi.fn().mockImplementationOnce(()=>new Promise<void>(resolve=>{finish=resolve})).mockResolvedValue(undefined);
 const store={read:vi.fn().mockResolvedValue(null),write} as unknown as RelightAnalysisAttemptStore,submitRelightAnalysis=vi.fn().mockResolvedValue({run_id:id,workflow_id:other,state:'queued'}),onSubmitted=vi.fn();
 const props={api:{submitRelightAnalysis} as unknown as OwnerAPI,project:id,intent,store,available:false,labels,verifyTicket:vi.fn().mockResolvedValue(undefined),onSubmitted};
 const view=render(<RelightAnalysisAction {...props} capabilityAvailable/>);await screen.findByText('Prepare');await clickWhenEnabled(await screen.findByRole('button',{name:'Prepare'}));await waitFor(()=>expect(write).toHaveBeenCalledOnce());const saved=write.mock.calls[0][0];
 view.rerender(<RelightAnalysisAction {...props} capabilityAvailable={false}/>);await act(async()=>finish());await screen.findByText('Unknown');expect(submitRelightAnalysis).not.toHaveBeenCalled();
 view.rerender(<RelightAnalysisAction {...props} capabilityAvailable/>);await clickWhenEnabled(await screen.findByRole('button',{name:'Retry'}));await waitFor(()=>expect(onSubmitted).toHaveBeenCalledOnce());expect(submitRelightAnalysis.mock.calls[0].slice(1,3)).toEqual([saved.body,saved.key]);
});
it('does not navigate accepted analysis when hard gate changes during verification',async()=>{
 let finish!:()=>void;const verify=vi.fn(()=>new Promise<void>(resolve=>{finish=resolve})),onSubmitted=vi.fn();
 const store={read:vi.fn().mockResolvedValue({schema_version:1,project:id,key:'saved-key',body:intent,ticket:{run_id:id,workflow_id:other,state:'queued'}})} as unknown as RelightAnalysisAttemptStore;
 const props={api:{} as OwnerAPI,project:id,intent,store,available:false,labels,verifyTicket:verify,onSubmitted};
 const view=render(<RelightAnalysisAction {...props}/>);await clickWhenEnabled(await screen.findByRole('button',{name:'Open'}));await waitFor(()=>expect(verify).toHaveBeenCalledOnce());view.rerender(<RelightAnalysisAction {...props} disabled/>);await act(async()=>finish());expect(onSubmitted).not.toHaveBeenCalled();
});

it('flushes a received analysis ticket after storage failure even when worker disappears',async()=>{
 const write=vi.fn().mockResolvedValueOnce(undefined).mockRejectedValueOnce(new Error('disk full')).mockResolvedValue(undefined),submitRelightAnalysis=vi.fn().mockResolvedValue({run_id:id,workflow_id:other,state:'queued'}),onSubmitted=vi.fn();
 const store={read:vi.fn().mockResolvedValue(null),write} as unknown as RelightAnalysisAttemptStore;
 const props={api:{submitRelightAnalysis} as unknown as OwnerAPI,project:id,intent,store,available:false,labels,verifyTicket:vi.fn().mockResolvedValue(undefined),onSubmitted};
 const view=render(<RelightAnalysisAction {...props} capabilityAvailable/>);await clickWhenEnabled(await screen.findByRole('button',{name:'Prepare'}));await screen.findByText('Unknown');expect(submitRelightAnalysis).toHaveBeenCalledOnce();
 view.rerender(<RelightAnalysisAction {...props} capabilityAvailable={false}/>);await clickWhenEnabled(await screen.findByRole('button',{name:'Open'}));await waitFor(()=>expect(onSubmitted).toHaveBeenCalledOnce());expect(write).toHaveBeenCalledTimes(3);expect(submitRelightAnalysis).toHaveBeenCalledOnce();
});

it('keeps uncertain journal and never navigates after replay-only absence',async()=>{
 const saved={schema_version:1,project:id,key:'recovery-key',body:intent,ticket:null};
 const submitRelightAnalysis=vi.fn().mockRejectedValue(new Error('410 receipt absent')),verify=vi.fn(),onSubmitted=vi.fn();
 const write=vi.fn().mockResolvedValue(undefined),clear=vi.fn(),store={read:vi.fn().mockResolvedValue(saved),write,clear} as unknown as RelightAnalysisAttemptStore;
 render(<RelightAnalysisAction api={{submitRelightAnalysis} as unknown as OwnerAPI} project={id} intent={intent} available={false} store={store} labels={labels} capabilityAvailable={false} verifyTicket={verify} onSubmitted={onSubmitted}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'Retry'}));await screen.findByText('Unknown');
 expect(submitRelightAnalysis).toHaveBeenCalledOnce();expect(submitRelightAnalysis.mock.calls[0][4]).toBe(true);expect(write.mock.calls[0][0]).toEqual(saved);
 expect(clear).not.toHaveBeenCalled();expect(verify).not.toHaveBeenCalled();expect(onSubmitted).not.toHaveBeenCalled();
});
it('does not upgrade replay-only to admission when capability returns during journal write',async()=>{
 const saved={schema_version:1,project:id,key:'recovery-key',body:intent,ticket:null};
 let finish!:()=>void;const write=vi.fn().mockImplementationOnce(()=>new Promise<void>(resolve=>{finish=resolve})).mockResolvedValue(undefined);
 const store={read:vi.fn().mockResolvedValue(saved),write} as unknown as RelightAnalysisAttemptStore;
 const submitRelightAnalysis=vi.fn().mockResolvedValue({run_id:id,workflow_id:other,state:'queued'}),verify=vi.fn().mockResolvedValue(undefined),onSubmitted=vi.fn();
 const props={api:{submitRelightAnalysis} as unknown as OwnerAPI,project:id,intent,available:false,store,labels,verifyTicket:verify,onSubmitted};
 const view=render(<RelightAnalysisAction {...props} capabilityAvailable={false}/>);await clickWhenEnabled(await screen.findByRole('button',{name:'Retry'}));await waitFor(()=>expect(write).toHaveBeenCalledOnce());
 view.rerender(<RelightAnalysisAction {...props} capabilityAvailable/>);await act(async()=>finish());await waitFor(()=>expect(onSubmitted).toHaveBeenCalledOnce());
 expect(submitRelightAnalysis.mock.calls[0][4]).toBe(true);expect(submitRelightAnalysis.mock.calls[0].slice(1,3)).toEqual([saved.body,saved.key]);
});
