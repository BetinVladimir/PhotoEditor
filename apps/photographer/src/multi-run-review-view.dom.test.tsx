// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,render,screen,waitFor} from '@testing-library/react';
const mocks=vi.hoisted(()=>({view:vi.fn(),active:true,generation:0}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>({active:mocks.active,generation:mocks.generation})}));
vi.mock('./multi-review-view',()=>({MultiReviewView:(p:unknown)=>{mocks.view(p);return <div>Multi candidate</div>;}}));
import {MultiRunReviewView} from './multi-run-review-view';
import {APIError} from '@photoeditor/api-client';
import type {OwnerAPI} from './owner-api';
afterEach(()=>{cleanup();vi.resetAllMocks();mocks.active=true;mocks.generation=0;});
const scope={tenant_id:'tenant',project_id:'project',reference_asset_id:'asset',run_id:'run',workflow_id:'workflow',task_id:'task',operation:'optical.panorama',intent:{schema_version:1}};
it('recovers the server-frozen review with no local submission journal or POST',async()=>{
 const api={me:vi.fn().mockResolvedValue({id:'actor'}),multiRunReview:vi.fn().mockResolvedValue(scope)},onOpen=vi.fn();
 render(<MultiRunReviewView api={api as unknown as OwnerAPI} origin="https://photo.test" tenant="tenant" run="run" onOpen={onOpen} onAccessDenied={vi.fn()}/>);
 await screen.findByText('Multi candidate');expect(api.multiRunReview).toHaveBeenCalledOnce();const props=mocks.view.mock.calls.at(-1)![0];expect(props.attempt.body).toEqual(scope.intent);expect(props.attempt.ticket).toEqual({run_id:'run',workflow_id:'workflow',state:'queued'});expect(props.apply.selection).toBeNull();expect(onOpen).not.toHaveBeenCalled();
 props.apply.onApplied({asset_version_id:'saved'});expect(onOpen).toHaveBeenCalledWith('asset','saved');
});
it.each([401,403,404,410])('withdraws denied history %s without retry',async(status)=>{
 const denied=vi.fn(),api={me:async()=>({id:'actor'}),multiRunReview:vi.fn().mockRejectedValue(new APIError(status,'FORBIDDEN'))};
 render(<MultiRunReviewView api={api as unknown as OwnerAPI} origin="o" tenant="t" run="r" onOpen={vi.fn()} onAccessDenied={denied}/>);
 await waitFor(()=>expect(denied).toHaveBeenCalledOnce());expect(mocks.view).not.toHaveBeenCalled();expect(api.multiRunReview).toHaveBeenCalledOnce();
});
it('discards a late background discovery response',async()=>{
 let finish!:(v:unknown)=>void;const api={me:async()=>({id:'actor'}),multiRunReview:vi.fn(()=>new Promise(r=>{finish=r;}))};
 const props={api:api as unknown as OwnerAPI,origin:'o',tenant:'t',run:'r',onOpen:vi.fn(),onAccessDenied:vi.fn()};const view=render(<MultiRunReviewView {...props}/>);await waitFor(()=>expect(api.multiRunReview).toHaveBeenCalledOnce());
 mocks.active=false;view.rerender(<MultiRunReviewView {...props}/>);await act(async()=>finish(scope));expect(mocks.view).not.toHaveBeenCalled();
});
it('Cancel whole discovery then retry rejects the old actor/result chain',async()=>{
 const {fireEvent}=await import('@testing-library/react');let finish!:(v:unknown)=>void;const api={me:vi.fn().mockResolvedValue({id:'actor'}),multiRunReview:vi.fn().mockReturnValueOnce(new Promise(done=>{finish=done})).mockResolvedValue(scope)};render(<MultiRunReviewView api={api as unknown as OwnerAPI} origin="o" tenant="t" run="r" onOpen={vi.fn()} onAccessDenied={vi.fn()}/>);await waitFor(()=>expect(api.multiRunReview).toHaveBeenCalledOnce());fireEvent.click(screen.getByText('Stop loading'));fireEvent.click(screen.getByText('Retry loading'));await screen.findByText('Multi candidate');const calls=mocks.view.mock.calls.length;await act(async()=>finish({...scope,workflow_id:'stale'}));expect(mocks.view.mock.calls.length).toBe(calls);
});
