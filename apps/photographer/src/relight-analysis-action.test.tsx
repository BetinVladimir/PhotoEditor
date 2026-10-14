// @vitest-environment jsdom
import React from 'react';
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
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
 await screen.findByText('Retry');fireEvent.click(screen.getByText('Retry'));
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
 await waitFor(()=>expect(screen.getByText('Prepare').closest('[aria-disabled="true"]')).toBeNull());
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
 await screen.findByText('Retry');fireEvent.click(screen.getByText('Retry'));
 await waitFor(()=>expect(api.submitRelightAnalysis).toHaveBeenCalledOnce());
 view.rerender(<RelightAnalysisAction {...shared} store={next}/>);
 await screen.findByText('Prepare');
 resolve({run_id:id,workflow_id:other,state:'queued'});
 await pending;
 expect(verify).not.toHaveBeenCalled();expect(navigate).not.toHaveBeenCalled();
});
