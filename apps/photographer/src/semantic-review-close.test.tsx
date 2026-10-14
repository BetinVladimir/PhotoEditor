// @vitest-environment jsdom
import React from 'react';
import {act,render,screen,waitFor,fireEvent,cleanup} from '@testing-library/react';
import {afterEach,it,expect,vi} from 'vitest';
vi.mock('react-native',async()=>import('react-native-web'));
import {APIError} from '@photoeditor/api-client';
import {SemanticReviewCloseControl} from './semantic-review-close';
const id='11111111-1111-4111-8111-111111111111';
const status={run_id:id,workflow_id:id,revision:'9007199254740993',state:'waiting_review' as const,plan_sha:'a'.repeat(64),remaining:0,can_close:true};
const closed={run_id:id,workflow_id:id,revision:'9007199254740994',state:'completed' as const,outputs:[] as [],error_code:null};
afterEach(cleanup);
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
