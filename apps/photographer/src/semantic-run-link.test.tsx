// @vitest-environment jsdom
import React from 'react';
import {act,cleanup,fireEvent,render,screen} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
const app=vi.hoisted(()=>({currentState:'active',change:null as null|((s:string)=>void)}));
vi.mock('react-native',async()=>({...await import('react-native-web'),AppState:{get currentState(){return app.currentState},addEventListener:(_event:string,callback:(s:string)=>void)=>{app.change=callback;return{remove:()=>{app.change=null}}}}}));
import {SemanticRunLink} from './semantic-run-link';
import {APIError} from '@photoeditor/api-client';
const run='11111111-1111-4111-8111-111111111111',workflow='22222222-2222-4222-8222-222222222222',intent='33333333-3333-4333-8333-333333333333';
const initial={run_id:run,workflow_id:workflow,intent_id:null,revision:'9007199254740993',state:'queued' as const};
afterEach(()=>{cleanup();vi.useRealTimers();app.currentState='active';app.change=null});
async function flush(){await act(async()=>{await Promise.resolve();await Promise.resolve()})}
it('opens actual independent intent after pending processing',async()=>{
 vi.useFakeTimers();const client={semanticRunIntent:vi.fn().mockResolvedValueOnce(initial).mockResolvedValue({...initial,intent_id:intent,state:'waiting_review',revision:'9007199254740994'})},onOpen=vi.fn();
 render(<SemanticRunLink client={client} tenant={run} run={run} workflow={workflow} onOpen={onOpen}/>);await flush();
 expect(screen.queryByRole('button')).toBeNull();
 await act(async()=>{await vi.advanceTimersByTimeAsync(3000)});
 fireEvent.click(screen.getByText('Open batch review'));expect(onOpen).toHaveBeenCalledWith(intent);
});
it.each(['workflow','run','revision','intent'])('rejects changed %s without exposing navigation',async(kind)=>{
 vi.useFakeTimers();const stable={...initial,intent_id:intent};let changed={...stable};
 if(kind==='workflow')changed.workflow_id=run;if(kind==='run')changed.run_id=workflow;if(kind==='revision')changed.revision='9007199254740992';if(kind==='intent')changed.intent_id=workflow;
 const client={semanticRunIntent:vi.fn().mockResolvedValueOnce(stable).mockResolvedValue(changed)};
 render(<SemanticRunLink client={client} tenant={run} run={run} workflow={workflow} onOpen={vi.fn()}/>);await flush();
 await act(async()=>{await vi.advanceTimersByTimeAsync(3000)});
 expect(screen.queryByText('Open batch review')).toBeNull();expect(screen.getByRole('alert')).toBeTruthy();
});
it('aborts background reads and ignores late results on scope change',async()=>{
 vi.useFakeTimers();let finish:(v:unknown)=>void=()=>{};
 const client={semanticRunIntent:vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValue(initial)};
 const view=render(<SemanticRunLink client={client} tenant={run} run={run} workflow={workflow} onOpen={vi.fn()}/>);await flush();
 const signal=client.semanticRunIntent.mock.calls[0][2] as AbortSignal;
 act(()=>app.change?.('background'));expect(signal.aborted).toBe(true);
 act(()=>app.change?.('active'));await flush();expect(client.semanticRunIntent).toHaveBeenCalledTimes(2);
 view.rerender(<SemanticRunLink client={client} tenant={intent} run={run} workflow={workflow} onOpen={vi.fn()}/>);await flush();
 await act(async()=>finish({...initial,intent_id:intent}));expect(screen.queryByText('Open batch review')).toBeNull();
});
it('removes private link on access loss and retries reads only',async()=>{
 vi.useFakeTimers();const client={semanticRunIntent:vi.fn().mockResolvedValueOnce({...initial,intent_id:intent}).mockRejectedValueOnce(new APIError(403,'FORBIDDEN')).mockResolvedValue(initial)};
 render(<SemanticRunLink client={client} tenant={run} run={run} workflow={workflow} onOpen={vi.fn()}/>);await flush();
 await act(async()=>{await vi.advanceTimersByTimeAsync(3000)});expect(screen.queryByText('Open batch review')).toBeNull();
 fireEvent.click(screen.getByText('Refresh'));await flush();expect(client.semanticRunIntent).toHaveBeenCalledTimes(3);
});
it('stops terminal polling and does not display preparation after failure',async()=>{
 vi.useFakeTimers();const client={semanticRunIntent:vi.fn().mockResolvedValue({...initial,state:'failed'})};
 render(<SemanticRunLink client={client} tenant={run} run={run} workflow={workflow} onOpen={vi.fn()}/>);await flush();
 expect(screen.queryByText('Preparing batch review…')).toBeNull();
 await act(async()=>{await vi.advanceTimersByTimeAsync(30000)});expect(client.semanticRunIntent).toHaveBeenCalledTimes(1);
});
it('keeps immutable intent identity across denial and explicit refresh',async()=>{
 vi.useFakeTimers();const client={semanticRunIntent:vi.fn().mockResolvedValueOnce({...initial,intent_id:intent}).mockRejectedValueOnce(new APIError(403,'FORBIDDEN')).mockResolvedValue({...initial,intent_id:workflow})};
 render(<SemanticRunLink client={client} tenant={run} run={run} workflow={workflow} onOpen={vi.fn()}/>);await flush();
 await act(async()=>{await vi.advanceTimersByTimeAsync(3000)});fireEvent.click(screen.getByText('Refresh'));await flush();
 expect(screen.queryByText('Open batch review')).toBeNull();expect(screen.getByRole('alert')).toBeTruthy();
});
