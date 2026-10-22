// @vitest-environment jsdom
import React from 'react';
import {AppState,type AppStateStatus} from 'react-native';
import {act,render,screen,waitFor,fireEvent,cleanup} from '@testing-library/react';
import {afterEach,it,expect,vi} from 'vitest';
vi.mock('react-native',async()=>import('react-native-web'));
import {APIError} from '@photoeditor/api-client';
import {SemanticReviewCloseControl} from './semantic-review-close';
const id='11111111-1111-4111-8111-111111111111';
const status={run_id:id,workflow_id:id,revision:'9007199254740990',state:'waiting_review' as const,plan_sha:'a'.repeat(64),remaining:0,can_close:true};
const closed={run_id:id,workflow_id:id,revision:'9007199254740991',state:'completed' as const,outputs:[] as [],error_code:null};
afterEach(()=>{cleanup();vi.restoreAllMocks()});
it('retries an uncertain closure with the original fence and key',async()=>{
 const semanticReviewStatus=vi.fn().mockResolvedValue(status);
 const closeSemanticReview=vi.fn().mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce(closed);
 render(<SemanticReviewCloseControl client={{semanticReviewStatus,closeSemanticReview}} tenant={id} intent={id}/>);
 await waitFor(()=>expect(screen.getByText('Close review')).toBeTruthy());
 fireEvent.click(screen.getByText('Close review'));
 await waitFor(()=>expect(screen.getByRole('button',{name:'Retry closing review'}).hasAttribute('disabled')).toBe(false));
 fireEvent.click(screen.getByText('Retry closing review'));
 await waitFor(()=>expect(screen.getByText(/Review closed/)).toBeTruthy());
 expect(closeSemanticReview.mock.calls[0].slice(0,4)).toEqual(closeSemanticReview.mock.calls[1].slice(0,4));
 expect(closeSemanticReview.mock.calls[0][2]).toEqual({expected_revision:status.revision,plan_sha:status.plan_sha});
 expect(screen.queryByText('Close review')).toBeNull();
});
it('requires a fresh fence after revision conflict',async()=>{
 const semanticReviewStatus=vi.fn().mockResolvedValue(status),closeSemanticReview=vi.fn().mockRejectedValue(new APIError(409,'REVISION_CONFLICT'));
 render(<SemanticReviewCloseControl client={{semanticReviewStatus,closeSemanticReview}} tenant={id} intent={id}/>);
 await waitFor(()=>expect(screen.getByText('Close review')).toBeTruthy());fireEvent.click(screen.getByText('Close review'));
 await waitFor(()=>expect(screen.getByRole('alert')).toBeTruthy());expect(screen.queryByText('Close review')).toBeNull();expect(screen.queryByText('Retry closing review')).toBeNull();
});
it('does not close unresolved targets and aborts state lookup on unmount',async()=>{
 const semanticReviewStatus=vi.fn().mockResolvedValue({...status,remaining:1,can_close:false}),closeSemanticReview=vi.fn();
 const view=render(<SemanticReviewCloseControl client={{semanticReviewStatus,closeSemanticReview}} tenant={id} intent={id}/>);
 await waitFor(()=>expect(screen.getByText('1 photos still need a decision.')).toBeTruthy());expect(screen.queryByText('Close review')).toBeNull();view.unmount();
 const pending=vi.fn().mockImplementation(()=>new Promise(()=>{}));
 const next=render(<SemanticReviewCloseControl client={{semanticReviewStatus:pending,closeSemanticReview}} tenant={id} intent={id}/>);
 await waitFor(()=>expect(pending).toHaveBeenCalledOnce());const signal=pending.mock.calls[0][2] as AbortSignal;next.unmount();expect(signal.aborted).toBe(true);
});

it('discards old uncertain close authority on client replacement',async()=>{const first={semanticReviewStatus:vi.fn().mockResolvedValue(status),closeSemanticReview:vi.fn().mockRejectedValue(new Error('lost'))};const view=render(<SemanticReviewCloseControl client={first} tenant={id} intent={id}/>);const button=await screen.findByRole('button',{name:'Close review'});await waitFor(()=>expect(button.hasAttribute('disabled')).toBe(false));fireEvent.click(button);const retry=await screen.findByRole('button',{name:'Retry closing review'});await waitFor(()=>expect(retry.hasAttribute('disabled')).toBe(false));const original=first.closeSemanticReview.mock.calls[0];const next={semanticReviewStatus:vi.fn().mockResolvedValue({...status,revision:'20',plan_sha:'b'.repeat(64)}),closeSemanticReview:vi.fn().mockResolvedValue({...closed,revision:'21'})};view.rerender(<SemanticReviewCloseControl client={next} tenant={id} intent={id}/>);const fresh=await screen.findByRole('button',{name:'Close review'});await waitFor(()=>expect(fresh.hasAttribute('disabled')).toBe(false));expect(screen.queryByText('Retry closing review')).toBeNull();fireEvent.click(fresh);await waitFor(()=>expect(next.closeSemanticReview).toHaveBeenCalledTimes(1));expect(next.closeSemanticReview.mock.calls[0][2]).toEqual({expected_revision:'20',plan_sha:'b'.repeat(64)});expect(next.closeSemanticReview.mock.calls[0][3]).not.toBe(original[3]);});
it('does not mark the new scope completed from a late old close response',async()=>{let finish!:(value:unknown)=>void;const first={semanticReviewStatus:vi.fn().mockResolvedValue(status),closeSemanticReview:vi.fn().mockImplementation(()=>new Promise(resolve=>{finish=resolve;}))};const view=render(<SemanticReviewCloseControl client={first} tenant={id} intent={id}/>);const button=await screen.findByRole('button',{name:'Close review'});await waitFor(()=>expect(button.hasAttribute('disabled')).toBe(false));fireEvent.click(button);await waitFor(()=>expect(first.closeSemanticReview).toHaveBeenCalledTimes(1));const signal=first.closeSemanticReview.mock.calls[0][4] as AbortSignal,next={semanticReviewStatus:vi.fn().mockResolvedValue({...status,remaining:5,can_close:false}),closeSemanticReview:vi.fn()};view.rerender(<SemanticReviewCloseControl client={next} tenant={id} intent={id}/>);await screen.findByText('5 photos still need a decision.');await act(async()=>finish(closed));expect(signal.aborted).toBe(true);expect(screen.queryByText(/Review closed/)).toBeNull();expect(next.closeSemanticReview).not.toHaveBeenCalled();expect(screen.queryByText('Close review')).toBeNull();});
it.each([401,403,404])('drops uncertain close after authority refresh refuses HTTP%s',async(code)=>{
 const client={semanticReviewStatus:vi.fn().mockResolvedValueOnce(status).mockRejectedValueOnce(new APIError(code,'DENIED')),closeSemanticReview:vi.fn().mockRejectedValue(new Error('lost'))};
 render(<SemanticReviewCloseControl client={client} tenant={id} intent={id}/>);
 const close=await screen.findByRole('button',{name:'Close review'});await act(async()=>{});fireEvent.click(close);
 await screen.findByRole('button',{name:'Retry closing review'});await act(async()=>{});
 fireEvent.click(screen.getByRole('button',{name:'Refresh review status'}));
 await waitFor(()=>expect(client.semanticReviewStatus).toHaveBeenCalledTimes(2));
 await waitFor(()=>expect(screen.queryByRole('button',{name:'Retry closing review'})).toBeNull());
 expect(screen.queryByRole('button',{name:'Close review'})).toBeNull();expect(client.closeSemanticReview).toHaveBeenCalledTimes(1);
});
it('retains exact uncertain close through a transient authority-read failure',async()=>{
 const client={semanticReviewStatus:vi.fn().mockResolvedValueOnce(status).mockRejectedValueOnce(new Error('offline')),closeSemanticReview:vi.fn().mockRejectedValueOnce(new Error('lost')).mockResolvedValueOnce(closed)};
 render(<SemanticReviewCloseControl client={client} tenant={id} intent={id}/>);
 await screen.findByRole('button',{name:'Close review'});await act(async()=>{});fireEvent.click(screen.getByText('Close review'));
 await screen.findByText('Retry closing review');await act(async()=>{});const original=client.closeSemanticReview.mock.calls[0].slice(0,4);
 fireEvent.click(screen.getByText('Refresh review status'));await waitFor(()=>expect(client.semanticReviewStatus).toHaveBeenCalledTimes(2));await act(async()=>{});
 fireEvent.click(screen.getByText('Retry closing review'));await waitFor(()=>expect(client.closeSemanticReview).toHaveBeenCalledTimes(2));expect(client.closeSemanticReview.mock.calls[1].slice(0,4)).toEqual(original);
});

it.each(['tenant','intent'] as const)('drops uncertain close identity when %s changes',async(scope)=>{
 const other='22222222-2222-4222-8222-222222222222';
 const client={semanticReviewStatus:vi.fn().mockResolvedValue(status),closeSemanticReview:vi.fn().mockRejectedValue(new Error('lost'))};
 const view=render(<SemanticReviewCloseControl client={client} tenant={id} intent={id}/>);
 const first=await screen.findByRole('button',{name:'Close review'});
 await waitFor(()=>expect(first.hasAttribute('disabled')).toBe(false));fireEvent.click(first);
 await screen.findByRole('button',{name:'Retry closing review'});
 const original=client.closeSemanticReview.mock.calls[0];
 view.rerender(<SemanticReviewCloseControl client={client} tenant={scope==='tenant'?other:id} intent={scope==='intent'?other:id}/>);
 const fresh=await screen.findByRole('button',{name:'Close review'});
 await waitFor(()=>expect(fresh.hasAttribute('disabled')).toBe(false));
 expect(screen.queryByRole('button',{name:'Retry closing review'})).toBeNull();
 fireEvent.click(fresh);
 await waitFor(()=>expect(client.closeSemanticReview).toHaveBeenCalledTimes(2));
 const next=client.closeSemanticReview.mock.calls[1];
 expect(next[scope==='tenant'?0:1]).toBe(other);
 expect(next[3]).not.toBe(original[3]);
});

it('keeps the original uncertain close when refresh returns a newer fence',async()=>{
 const client={semanticReviewStatus:vi.fn().mockResolvedValueOnce(status).mockResolvedValueOnce({...status,revision:'20',plan_sha:'b'.repeat(64)}),closeSemanticReview:vi.fn().mockRejectedValueOnce(new Error('lost')).mockResolvedValueOnce(closed)};
 render(<SemanticReviewCloseControl client={client} tenant={id} intent={id}/>);
 const button=await screen.findByRole('button',{name:'Close review'});
 await waitFor(()=>expect(button.hasAttribute('disabled')).toBe(false));fireEvent.click(button);
 const retry=await screen.findByRole('button',{name:'Retry closing review'});
 await waitFor(()=>expect(retry.hasAttribute('disabled')).toBe(false));
 const original=client.closeSemanticReview.mock.calls[0].slice(0,4);
 fireEvent.click(screen.getByRole('button',{name:'Refresh review status'}));
 await waitFor(()=>expect(client.semanticReviewStatus).toHaveBeenCalledTimes(2));
 await waitFor(()=>expect(screen.getByRole('button',{name:'Retry closing review'}).hasAttribute('disabled')).toBe(false));
 fireEvent.click(screen.getByRole('button',{name:'Retry closing review'}));
 await waitFor(()=>expect(client.closeSemanticReview).toHaveBeenCalledTimes(2));
 expect(client.closeSemanticReview.mock.calls[1].slice(0,4)).toEqual(original);
 await screen.findByText(/Review closed/);
});

it('blocks close while decision recovery is pending and enables it after recovery',async()=>{
 const client={semanticReviewStatus:vi.fn().mockResolvedValue(status),closeSemanticReview:vi.fn().mockResolvedValue(closed)};
 const view=render(<SemanticReviewCloseControl client={client} tenant={id} intent={id} blocked/>);
 const button=await screen.findByRole('button',{name:'Close review'});
 expect(button.hasAttribute('disabled')).toBe(true);fireEvent.click(button);
 expect(client.closeSemanticReview).not.toHaveBeenCalled();
 view.rerender(<SemanticReviewCloseControl client={client} tenant={id} intent={id} blocked={false}/>);
 fireEvent.click(screen.getByText('Close review'));
 await waitFor(()=>expect(client.closeSemanticReview).toHaveBeenCalledOnce());
});
it('preserves uncertain close request while decision coordination blocks retry',async()=>{
 const client={semanticReviewStatus:vi.fn().mockResolvedValue(status),closeSemanticReview:vi.fn().mockRejectedValueOnce(new Error('lost')).mockResolvedValueOnce(closed)};
 const view=render(<SemanticReviewCloseControl client={client} tenant={id} intent={id}/>);
 fireEvent.click(await screen.findByText('Close review'));
 await waitFor(()=>expect(screen.getByRole('button',{name:'Retry closing review'}).hasAttribute('disabled')).toBe(false));
 const original=structuredClone(client.closeSemanticReview.mock.calls[0].slice(0,4));
 view.rerender(<SemanticReviewCloseControl client={client} tenant={id} intent={id} blocked/>);
 fireEvent.click(screen.getByText('Retry closing review'));expect(client.closeSemanticReview).toHaveBeenCalledOnce();
 view.rerender(<SemanticReviewCloseControl client={client} tenant={id} intent={id} blocked={false}/>);
 fireEvent.click(screen.getByText('Retry closing review'));
 await waitFor(()=>expect(client.closeSemanticReview).toHaveBeenCalledTimes(2));
 expect(client.closeSemanticReview.mock.calls[1].slice(0,4)).toEqual(original);
});

it('refreshes authority after background even when an aborted read never settles',async()=>{
 const listeners=new Set<(state:AppStateStatus)=>void>();
 vi.spyOn(AppState,'addEventListener').mockImplementation((_event,listener)=>{listeners.add(listener);return {remove:()=>listeners.delete(listener)}});
 let old!:(value:unknown)=>void;
 const client={semanticReviewStatus:vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{old=resolve})).mockRejectedValueOnce(new APIError(403,'ACCESS_DENIED')),closeSemanticReview:vi.fn()};
 render(<SemanticReviewCloseControl client={client} tenant={id} intent={id}/>);
 await waitFor(()=>expect(client.semanticReviewStatus).toHaveBeenCalledOnce());
 const signal=client.semanticReviewStatus.mock.calls[0][2] as AbortSignal;
 act(()=>listeners.forEach(listener=>listener('background')));expect(signal.aborted).toBe(true);
 act(()=>listeners.forEach(listener=>listener('active')));
 await waitFor(()=>expect(client.semanticReviewStatus).toHaveBeenCalledTimes(2));
 await act(async()=>old(status));
 expect(screen.queryByText('Close review')).toBeNull();expect(client.closeSemanticReview).not.toHaveBeenCalled();
});
it('preserves exact uncertain close across background and ignores late old completion',async()=>{
 const listeners=new Set<(state:AppStateStatus)=>void>();
 vi.spyOn(AppState,'addEventListener').mockImplementation((_event,listener)=>{listeners.add(listener);return {remove:()=>listeners.delete(listener)}});
 let old!:(value:unknown)=>void;
 const client={semanticReviewStatus:vi.fn().mockResolvedValue(status),closeSemanticReview:vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{old=resolve})).mockResolvedValueOnce(closed)};
 render(<SemanticReviewCloseControl client={client} tenant={id} intent={id}/>);
 fireEvent.click(await screen.findByText('Close review'));
 await waitFor(()=>expect(client.closeSemanticReview).toHaveBeenCalledOnce());
 const original=structuredClone(client.closeSemanticReview.mock.calls[0].slice(0,4));
 const signal=client.closeSemanticReview.mock.calls[0][4] as AbortSignal;
 act(()=>listeners.forEach(listener=>listener('background')));
 expect(signal.aborted).toBe(true);
 const retry=screen.getByRole('button',{name:'Retry closing review'});
 expect(retry.hasAttribute('disabled')).toBe(true);fireEvent.click(retry);
 expect(client.closeSemanticReview).toHaveBeenCalledOnce();
 await act(async()=>old(closed));expect(screen.queryByText(/Review closed/)).toBeNull();
 act(()=>listeners.forEach(listener=>listener('active')));
 await waitFor(()=>expect(screen.getByRole('button',{name:'Retry closing review'}).hasAttribute('disabled')).toBe(false));
 fireEvent.click(screen.getByText('Retry closing review'));
 await waitFor(()=>expect(client.closeSemanticReview).toHaveBeenCalledTimes(2));
 expect(client.closeSemanticReview.mock.calls[1].slice(0,4)).toEqual(original);
});

it('retains exact close identity after Stop and ignores old completion during a pending retry',async()=>{
 let first!:(value:typeof closed)=>void,second!:(value:typeof closed)=>void;
 const semanticReviewStatus=vi.fn().mockResolvedValue(status),closeSemanticReview=vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{first=resolve})).mockImplementationOnce(()=>new Promise(resolve=>{second=resolve}));
 render(<SemanticReviewCloseControl client={{semanticReviewStatus,closeSemanticReview}} tenant={id} intent={id}/>);await screen.findByText('Close review');fireEvent.click(screen.getByText('Close review'));
 await waitFor(()=>expect(closeSemanticReview).toHaveBeenCalledOnce());fireEvent.click(screen.getByText('Stop loading'));fireEvent.click(screen.getByText('Retry closing review'));await waitFor(()=>expect(closeSemanticReview).toHaveBeenCalledTimes(2));
 await act(async()=>first(closed));expect(screen.queryByText(/Review closed/)).toBeNull();expect(screen.getByText('Stop loading')).toBeTruthy();
 await act(async()=>second(closed));await screen.findByText(/Review closed/);expect(closeSemanticReview.mock.calls[0].slice(0,4)).toEqual(closeSemanticReview.mock.calls[1].slice(0,4));
});
it('remount reconciles an already completed close through status without another POST',async()=>{
 const semanticReviewStatus=vi.fn().mockResolvedValue({...status,state:'completed',plan_sha:null,can_close:false}),closeSemanticReview=vi.fn();
 render(<SemanticReviewCloseControl client={{semanticReviewStatus,closeSemanticReview}} tenant={id} intent={id}/>);await screen.findByText(/Review closed/);expect(closeSemanticReview).not.toHaveBeenCalled();
});
