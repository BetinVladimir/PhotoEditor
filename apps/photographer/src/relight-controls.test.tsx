// @vitest-environment jsdom
import React from 'react';
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
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
 await waitFor(()=>expect(screen.getByText('Submit').closest('[role="button"]')?.getAttribute('aria-disabled')).not.toBe('true'));
 fireEvent.click(screen.getByText('Submit'));
 await screen.findByText('Unknown result');
 fireEvent.click(screen.getByText('Retry'));
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
 fireEvent.click(await screen.findByText('Another correction'));
 await waitFor(()=>expect(another).toHaveBeenCalledOnce());
 expect(clear).toHaveBeenCalledWith('accepted-key');expect(verify).toHaveBeenCalledTimes(2);
 expect(verify.mock.invocationCallOrder[1]).toBeLessThan(clear.mock.invocationCallOrder[0]);
});
