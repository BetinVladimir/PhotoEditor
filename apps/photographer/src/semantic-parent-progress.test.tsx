// @vitest-environment jsdom
import React from 'react';
import {act,cleanup,fireEvent,render,screen} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
const app=vi.hoisted(()=>({currentState:'active',change:null as null|((s:string)=>void)}));
vi.mock('react-native',async()=>({...await import('react-native-web'),AppState:{get currentState(){return app.currentState},addEventListener:(_event:string,callback:(s:string)=>void)=>{app.change=callback;return{remove:()=>{app.change=null}}}}}));
import {SemanticParentProgress} from './semantic-parent-progress';
import {APIError} from '@photoeditor/api-client';
const id='11111111-1111-4111-8111-111111111111',other='22222222-2222-4222-8222-222222222222';
const initial={run_id:id,workflow_id:id,revision:'9007199254740989',state:'queued' as const,plan_sha:null,remaining:0,can_close:false};
afterEach(()=>{cleanup();vi.useRealTimers();app.currentState='active';app.change=null});
async function flush(){await act(async()=>{await Promise.resolve();await Promise.resolve()})}
it('refreshes targets once when original parent becomes reviewable and stops polling',async()=>{
 vi.useFakeTimers();const client={semanticReviewStatus:vi.fn().mockResolvedValueOnce(initial).mockResolvedValue({...initial,revision:'9007199254740990',state:'waiting_review'})},onReady=vi.fn();
 render(<SemanticParentProgress client={client} tenant={id} intent={id} onReady={onReady}/>);await flush();
 expect(onReady).not.toHaveBeenCalled();expect(screen.getByText(/Batch status:/).textContent).toContain('Queued');
 await act(async()=>{await vi.advanceTimersByTimeAsync(3000)});
 expect(onReady).toHaveBeenCalledTimes(1);
 await act(async()=>{await vi.advanceTimersByTimeAsync(30000)});expect(client.semanticReviewStatus).toHaveBeenCalledTimes(2);
});
it('cancels background IO and resumes in the foreground',async()=>{
 vi.useFakeTimers();const client={semanticReviewStatus:vi.fn().mockImplementationOnce(()=>new Promise(()=>{})).mockResolvedValue(initial)};
 render(<SemanticParentProgress client={client} tenant={id} intent={id} onReady={vi.fn()}/>);await flush();
 const signal=client.semanticReviewStatus.mock.calls[0][2] as AbortSignal;
 act(()=>app.change?.('background'));expect(signal.aborted).toBe(true);
 await act(async()=>{await vi.advanceTimersByTimeAsync(30000)});expect(client.semanticReviewStatus).toHaveBeenCalledTimes(1);
 act(()=>app.change?.('active'));await flush();expect(client.semanticReviewStatus).toHaveBeenCalledTimes(2);
});
it('pins returned workflow independently of intent ID and rejects substitution on retry',async()=>{
 vi.useFakeTimers();const client={semanticReviewStatus:vi.fn().mockResolvedValueOnce({...initial,workflow_id:other}).mockResolvedValueOnce(initial).mockResolvedValue({...initial,workflow_id:other,revision:'9007199254740990',state:'waiting_review'})},onReady=vi.fn();
 render(<SemanticParentProgress client={client} tenant={id} intent={id} onReady={onReady}/>);await flush();
 await act(async()=>{await vi.advanceTimersByTimeAsync(3000)});
 expect(screen.getByRole('alert')).toBeTruthy();expect(onReady).not.toHaveBeenCalled();
 fireEvent.click(screen.getByText('Refresh'));await flush();expect(onReady).toHaveBeenCalledTimes(1);
});
it('retains original run and rejects a revision regression after refresh',async()=>{
 vi.useFakeTimers();const client={semanticReviewStatus:vi.fn().mockResolvedValueOnce(initial).mockResolvedValueOnce({...initial,revision:'9007199254740988'}).mockResolvedValue({...initial,run_id:other})},onReady=vi.fn();
 render(<SemanticParentProgress client={client} tenant={id} intent={id} onReady={onReady}/>);await flush();
 await act(async()=>{await vi.advanceTimersByTimeAsync(3000)});expect(screen.getByRole('alert')).toBeTruthy();
 fireEvent.click(screen.getByText('Refresh'));await flush();expect(screen.getByRole('alert')).toBeTruthy();expect(onReady).not.toHaveBeenCalled();
});
it('clears retained display on access loss and notifies the screen to remove private results',async()=>{
 vi.useFakeTimers();const client={semanticReviewStatus:vi.fn().mockResolvedValueOnce(initial).mockRejectedValue(new APIError(403,'ACCESS_DENIED'))},onAccessLost=vi.fn();
 render(<SemanticParentProgress client={client} tenant={id} intent={id} onReady={vi.fn()} onAccessLost={onAccessLost}/>);await flush();
 await act(async()=>{await vi.advanceTimersByTimeAsync(3000)});
 expect(onAccessLost).toHaveBeenCalledTimes(1);expect(screen.queryByText(/Batch status:/)).toBeNull();
});

it('bounded stalled parent polling exposes Refresh and ignores its late old status',async()=>{
 vi.useFakeTimers();let finish!:(value:typeof initial)=>void;const client={semanticReviewStatus:vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValue({...initial,state:'completed'})},onReady=vi.fn();
 render(<SemanticParentProgress client={client} tenant={id} intent={id} onReady={onReady}/>);await flush();await act(async()=>{await vi.advanceTimersByTimeAsync(8001)});
 expect(screen.getByRole('alert')).toBeTruthy();fireEvent.click(screen.getByText('Refresh'));await flush();expect(client.semanticReviewStatus).toHaveBeenCalledTimes(2);
 await act(async()=>finish(initial));expect(screen.getByText(/Batch status:/).textContent).not.toContain('Queued');expect(onReady).not.toHaveBeenCalled();
});
