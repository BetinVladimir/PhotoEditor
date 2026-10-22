// @vitest-environment jsdom
import {clickWhenEnabled,expectEnabled} from './test/ui-readiness';
import React from 'react';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
vi.mock('react-native',async()=>await import('react-native-web'));
import {RelightControls,type RelightLabels} from './relight-controls';
import type {RelightIntent} from './relight-intent';
import type {RelightAttemptStore} from './relight-attempt-store';
import type {OwnerAPI} from './owner-api';
const id='11111111-1111-4111-8111-111111111111',other='22222222-2222-4222-8222-222222222222';
const labels:RelightLabels={title:'Relight',approximate:'Approximate lighting',strength:'Strength',light:'Light',x:'X',y:'Y',radius:'Radius',ev:'EV',add:'Add light',remove:'Remove light',submit:'Submit',retry:'Retry',open:'Open',loading:'Loading',unknown:'Unknown result',invalid:'Invalid controls',unavailable:'Unavailable',another:'Another correction'};
function prepared():RelightIntent{const ref={artifact_id:id,sha256:'a'.repeat(64)};return {schema_version:1,snapshot_id:id,ordinal:0,subject_kind:'manual',params:{schema_version:1,base:{asset_version_id:id,source:ref,canvas_hash:'b'.repeat(64)},strength:.5,depth:ref,subject:ref,protected:ref,calibration:ref,lights:[{x:.5,y:.5,radius:.3,ev:0}]}};}
afterEach(cleanup);
it('retries ambiguous submission with identical body/key and validates ticket before navigation',async()=>{
 const submitRelight=vi.fn().mockRejectedValueOnce(new Error('network')).mockResolvedValue({run_id:id,workflow_id:other,state:'queued'}),write=vi.fn().mockResolvedValue(undefined),verify=vi.fn().mockResolvedValue(undefined),onSubmitted=vi.fn();
 const store={read:vi.fn().mockResolvedValue(null),write} as unknown as RelightAttemptStore;
 render(<RelightControls api={{submitRelight} as unknown as OwnerAPI} project={id} prepared={prepared()} store={store} labels={labels} verifyTicket={verify} onSubmitted={onSubmitted}/>);
 await waitFor(()=>expectEnabled(screen.getByText('Submit')));
 await clickWhenEnabled(await screen.findByRole('button',{name:'Submit'}));
 await screen.findByText('Unknown result');
 await clickWhenEnabled(await screen.findByRole('button',{name:'Retry'}));
 await waitFor(()=>expect(onSubmitted).toHaveBeenCalledOnce());
 expect(submitRelight.mock.calls[0][1]).toEqual(submitRelight.mock.calls[1][1]);expect(submitRelight.mock.calls[0][2]).toBe(submitRelight.mock.calls[1][2]);
 expect(verify.mock.invocationCallOrder[0]).toBeLessThan(onSubmitted.mock.invocationCallOrder[0]);
});
it('blocks saved acknowledgement when authenticated validation fails',async()=>{
 const record={schema_version:1,project:id,key:'saved-key',body:prepared(),ticket:{run_id:id,workflow_id:other,state:'queued'}};
 const store={read:vi.fn().mockResolvedValue(record),write:vi.fn()} as unknown as RelightAttemptStore;
 const onSubmitted=vi.fn();
 render(<RelightControls api={{submitRelight:vi.fn()} as unknown as OwnerAPI} project={id} prepared={prepared()} store={store} labels={labels} verifyTicket={vi.fn().mockRejectedValue(new Error('revoked'))} onSubmitted={onSubmitted}/>);
 await screen.findByText('Unavailable');expect(onSubmitted).not.toHaveBeenCalled();
});

it('revalidates an accepted attempt before clearing it for another correction',async()=>{
 const body=prepared(),saved={schema_version:1,project:id,key:'accepted-key',body,ticket:{run_id:id,workflow_id:other,state:'queued'}};
 const clear=vi.fn().mockResolvedValue(undefined),verify=vi.fn().mockResolvedValue(undefined),another=vi.fn();
 const store={read:vi.fn().mockResolvedValue(saved),write:vi.fn(),clear} as unknown as RelightAttemptStore;
 render(<RelightControls api={{submitRelight:vi.fn()} as unknown as OwnerAPI} project={id} prepared={body} store={store} labels={labels} verifyTicket={verify} onSubmitted={vi.fn()} onAnother={another}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'Another correction'}));
 await waitFor(()=>expect(another).toHaveBeenCalledOnce());
 expect(clear).toHaveBeenCalledWith('accepted-key');expect(verify).toHaveBeenCalledTimes(2);
 expect(verify.mock.invocationCallOrder[1]).toBeLessThan(clear.mock.invocationCallOrder[0]);
});

it('rechecks live availability after journal persistence and retains the exact retry',async()=>{
 let finish!:()=>void;
 const write=vi.fn().mockImplementationOnce(()=>new Promise<void>(resolve=>{finish=resolve;})).mockResolvedValue(undefined);
 const store={read:vi.fn().mockResolvedValue(null),write} as unknown as RelightAttemptStore;
 const submitRelight=vi.fn().mockResolvedValue({run_id:id,workflow_id:other,state:'queued'});
 const props={api:{submitRelight} as unknown as OwnerAPI,project:id,prepared:prepared(),store,labels,verifyTicket:vi.fn().mockResolvedValue(undefined),onSubmitted:vi.fn()};
 const view=render(<RelightControls {...props}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'Submit'}));await waitFor(()=>expect(write).toHaveBeenCalledTimes(1));
 const staged=write.mock.calls[0][0];view.rerender(<RelightControls {...props} disabled/>);
 await act(async()=>finish());await screen.findByText('Unknown result');expect(submitRelight).not.toHaveBeenCalled();
 view.rerender(<RelightControls {...props}/>);await clickWhenEnabled(await screen.findByRole('button',{name:'Retry'}));
 await waitFor(()=>expect(props.onSubmitted).toHaveBeenCalledOnce());
 expect(submitRelight.mock.calls[0][1]).toEqual(staged.body);expect(submitRelight.mock.calls[0][2]).toBe(staged.key);
});

it('opens an accepted relight without worker capability and does not resubmit',async()=>{
 const ticket={run_id:id,workflow_id:other,state:'queued'},submitRelight=vi.fn(),verifyTicket=vi.fn().mockResolvedValue(undefined),onSubmitted=vi.fn();
 const store={read:vi.fn().mockResolvedValue({schema_version:1,project:id,key:'saved-key',body:prepared(),ticket}),write:vi.fn()} as unknown as RelightAttemptStore;
 render(<RelightControls api={{submitRelight} as unknown as OwnerAPI} project={id} prepared={prepared()} store={store} labels={labels} capabilityAvailable={false} verifyTicket={verifyTicket} onSubmitted={onSubmitted}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'Open'}));await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(ticket));expect(verifyTicket).toHaveBeenCalledTimes(2);expect(submitRelight).not.toHaveBeenCalled();
});
it('recovers unknown receipt without worker using replay-only and original body/key',async()=>{
 const ticket={run_id:id,workflow_id:other,state:'queued'},submitRelight=vi.fn().mockResolvedValue(ticket),verify=vi.fn().mockResolvedValue(undefined),onSubmitted=vi.fn();
 const saved={schema_version:1,project:id,key:'saved-key',body:prepared(),ticket:null},store={read:vi.fn().mockResolvedValue(saved),write:vi.fn().mockResolvedValue(undefined)} as unknown as RelightAttemptStore;
 render(<RelightControls api={{submitRelight} as unknown as OwnerAPI} project={id} prepared={prepared()} store={store}  labels={labels} capabilityAvailable={false} verifyTicket={verify} onSubmitted={onSubmitted}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'Retry'}));
 await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(ticket));
 expect(submitRelight).toHaveBeenCalledOnce();expect(submitRelight.mock.calls[0].slice(1,3)).toEqual([saved.body,saved.key]);expect(submitRelight.mock.calls[0][4]).toBe(true);
 expect(verify.mock.invocationCallOrder[0]).toBeLessThan(onSubmitted.mock.invocationCallOrder[0]);
});
it('rechecks relight worker capability after journal IO and preserves retry identity',async()=>{
 let finish!:()=>void;const write=vi.fn().mockImplementationOnce(()=>new Promise<void>(resolve=>{finish=resolve})).mockResolvedValue(undefined);
 const store={read:vi.fn().mockResolvedValue(null),write} as unknown as RelightAttemptStore,submitRelight=vi.fn().mockResolvedValue({run_id:id,workflow_id:other,state:'queued'});
 const props={api:{submitRelight} as unknown as OwnerAPI,project:id,prepared:prepared(),store,labels,verifyTicket:vi.fn().mockResolvedValue(undefined),onSubmitted:vi.fn()};
 const view=render(<RelightControls {...props} capabilityAvailable/>);await clickWhenEnabled(await screen.findByRole('button',{name:'Submit'}));await waitFor(()=>expect(write).toHaveBeenCalledOnce());const saved=write.mock.calls[0][0];
 view.rerender(<RelightControls {...props} capabilityAvailable={false}/>);await act(async()=>finish());await screen.findByText('Unknown result');expect(submitRelight).not.toHaveBeenCalled();
 view.rerender(<RelightControls {...props} capabilityAvailable/>);await clickWhenEnabled(await screen.findByRole('button',{name:'Retry'}));await waitFor(()=>expect(props.onSubmitted).toHaveBeenCalledOnce());expect(submitRelight.mock.calls[0].slice(1,3)).toEqual([saved.body,saved.key]);
});

it('flushes a received relight ticket after storage failure without needing worker capability',async()=>{
 const write=vi.fn().mockResolvedValueOnce(undefined).mockRejectedValueOnce(new Error('disk full')).mockResolvedValue(undefined),submitRelight=vi.fn().mockResolvedValue({run_id:id,workflow_id:other,state:'queued'});
 const store={read:vi.fn().mockResolvedValue(null),write} as unknown as RelightAttemptStore;
 const props={api:{submitRelight} as unknown as OwnerAPI,project:id,prepared:prepared(),store,labels,verifyTicket:vi.fn().mockResolvedValue(undefined),onSubmitted:vi.fn()};
 const view=render(<RelightControls {...props} capabilityAvailable/>);await clickWhenEnabled(await screen.findByRole('button',{name:'Submit'}));await screen.findByText('Unknown result');
 view.rerender(<RelightControls {...props} capabilityAvailable={false}/>);await clickWhenEnabled(await screen.findByRole('button',{name:'Retry'}));await waitFor(()=>expect(props.onSubmitted).toHaveBeenCalledOnce());expect(write).toHaveBeenCalledTimes(3);expect(submitRelight).toHaveBeenCalledOnce();
});

it('keeps uncertain journal and never navigates after replay-only absence',async()=>{
 const saved={schema_version:1,project:id,key:'recovery-key',body:prepared(),ticket:null};
 const submitRelight=vi.fn().mockRejectedValue(new Error('410 receipt absent')),verify=vi.fn(),onSubmitted=vi.fn();
 const write=vi.fn().mockResolvedValue(undefined),clear=vi.fn(),store={read:vi.fn().mockResolvedValue(saved),write,clear} as unknown as RelightAttemptStore;
 render(<RelightControls api={{submitRelight} as unknown as OwnerAPI} project={id} prepared={prepared()}  store={store} labels={labels} capabilityAvailable={false} verifyTicket={verify} onSubmitted={onSubmitted}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'Retry'}));await screen.findByText('Unknown result');
 expect(submitRelight).toHaveBeenCalledOnce();expect(submitRelight.mock.calls[0][4]).toBe(true);expect(write.mock.calls[0][0]).toEqual(saved);
 expect(clear).not.toHaveBeenCalled();expect(verify).not.toHaveBeenCalled();expect(onSubmitted).not.toHaveBeenCalled();
});
it('does not upgrade replay-only to admission when capability returns during journal write',async()=>{
 const saved={schema_version:1,project:id,key:'recovery-key',body:prepared(),ticket:null};
 let finish!:()=>void;const write=vi.fn().mockImplementationOnce(()=>new Promise<void>(resolve=>{finish=resolve})).mockResolvedValue(undefined);
 const store={read:vi.fn().mockResolvedValue(saved),write} as unknown as RelightAttemptStore;
 const submitRelight=vi.fn().mockResolvedValue({run_id:id,workflow_id:other,state:'queued'}),verify=vi.fn().mockResolvedValue(undefined),onSubmitted=vi.fn();
 const props={api:{submitRelight} as unknown as OwnerAPI,project:id,prepared:prepared(),store,labels,verifyTicket:verify,onSubmitted};
 const view=render(<RelightControls {...props} capabilityAvailable={false}/>);await clickWhenEnabled(await screen.findByRole('button',{name:'Retry'}));await waitFor(()=>expect(write).toHaveBeenCalledOnce());
 view.rerender(<RelightControls {...props} capabilityAvailable/>);await act(async()=>finish());await waitFor(()=>expect(onSubmitted).toHaveBeenCalledOnce());
 expect(submitRelight.mock.calls[0][4]).toBe(true);expect(submitRelight.mock.calls[0].slice(1,3)).toEqual([saved.body,saved.key]);
});

it('blocks relight on live capability withdrawal without rerender and preserves retry',async()=>{
 let available=true;
 let finish!:()=>void;
 const write=vi.fn().mockImplementationOnce(()=>new Promise<void>(resolve=>{finish=resolve;})).mockResolvedValue(undefined);
 const store={read:vi.fn().mockResolvedValue(null),write} as unknown as RelightAttemptStore;
 const submitRelight=vi.fn().mockResolvedValue({run_id:id,workflow_id:other,state:'queued'});
 const props={operationAvailable:()=>available,api:{submitRelight} as unknown as OwnerAPI,project:id,prepared:prepared(),store,labels,verifyTicket:vi.fn().mockResolvedValue(undefined),onSubmitted:vi.fn()};
 render(<RelightControls {...props}/>);
 await waitFor(()=>expectEnabled(screen.getByText('Submit')));
 await clickWhenEnabled(await screen.findByRole('button',{name:'Submit'}));await waitFor(()=>expect(write).toHaveBeenCalledTimes(1));
 const staged=write.mock.calls[0][0];available=false;
 await act(async()=>finish());await screen.findByText('Unknown result');expect(submitRelight).not.toHaveBeenCalled();
 available=true;await clickWhenEnabled(await screen.findByRole('button',{name:'Retry'}));
 await waitFor(()=>expect(props.onSubmitted).toHaveBeenCalledOnce());
 expect(submitRelight.mock.calls[0][1]).toEqual(staged.body);expect(submitRelight.mock.calls[0][2]).toBe(staged.key);
});

