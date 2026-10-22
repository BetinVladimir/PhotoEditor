// @vitest-environment jsdom
import React from 'react';
import {act,cleanup,fireEvent,render,screen} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
const renderedPresses=vi.hoisted(()=>[] as Array<()=>void>);
const renderedStops=vi.hoisted(()=>[] as Array<()=>void>);
const app=vi.hoisted(()=>({currentState:'active',change:null as null|((s:string)=>void)}));
vi.mock('react-native',async()=>{
 const native=await import('react-native-web'),React=await import('react');
 return {...native,
  
  AppState:{get currentState(){return app.currentState},addEventListener:(_event:string,callback:(s:string)=>void)=>{app.change=callback;return{remove:()=>{app.change=null}}}}
 };
});
import {SemanticRunLink} from './semantic-run-link';
import {APIError} from '@photoeditor/api-client';
const run='11111111-1111-4111-8111-111111111111',workflow='22222222-2222-4222-8222-222222222222',intent='33333333-3333-4333-8333-333333333333';
const initial={run_id:run,workflow_id:workflow,intent_id:null,revision:'9007199254740993',state:'queued' as const};
afterEach(()=>{cleanup();vi.useRealTimers();app.currentState='active';app.change=null;renderedPresses.length=0;renderedStops.length=0});
async function flush(){await act(async()=>{await Promise.resolve();await Promise.resolve()})}
it('opens actual independent intent after pending processing',async()=>{
 vi.useFakeTimers();const client={semanticRunIntent:vi.fn().mockResolvedValueOnce(initial).mockResolvedValue({...initial,intent_id:intent,state:'waiting_review',revision:'9007199254740994'})},onOpen=vi.fn();
 render(<SemanticRunLink client={client} tenant={run} run={run} workflow={workflow} onOpen={onOpen}/>);await flush();
 expect(screen.queryByText('Open batch review')).toBeNull();expect(screen.getByText('Stop loading')).toBeTruthy();
 await act(async()=>{await vi.advanceTimersByTimeAsync(3000)});
 fireEvent.click(screen.getByText('Open batch review'));expect(onOpen).toHaveBeenCalledWith(intent);
});
it('pins discovered workflow identity when the run URL has no hint',async()=>{
 vi.useFakeTimers();const client={semanticRunIntent:vi.fn().mockResolvedValueOnce(initial).mockResolvedValue({...initial,workflow_id:run,intent_id:intent,revision:'9007199254740994',state:'waiting_review'})};
 render(<SemanticRunLink client={client} tenant={run} run={run} onOpen={vi.fn()}/>);await flush();
 await act(async()=>{await vi.advanceTimersByTimeAsync(3000)});
 expect(screen.queryByText('Open batch review')).toBeNull();expect(screen.getByRole('alert')).toBeTruthy();
});
it('treats an initial 404 without a workflow hint as a nonsemantic run',async()=>{
 const client={semanticRunIntent:vi.fn().mockRejectedValue(new APIError(404,'NOT_FOUND'))};
 render(<SemanticRunLink client={client} tenant={run} run={run} onOpen={vi.fn()}/>);await flush();
 expect(screen.queryByRole('alert')).toBeNull();expect(screen.queryByText('Open batch review')).toBeNull();
 expect(screen.queryByText('Preparing batch review…')).toBeNull();
 expect(client.semanticRunIntent).toHaveBeenCalledTimes(1);
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
it('Stop cancels observation, Retry reacquires and late old replies cannot replace it',async()=>{
 let resolve!:(v:unknown)=>void;const client={semanticRunIntent:vi.fn().mockReturnValueOnce(new Promise(done=>{resolve=done})).mockResolvedValue({...initial,intent_id:intent,state:'completed'})};const onOpen=vi.fn();render(<SemanticRunLink client={client} tenant={run} run={run} workflow={workflow} onOpen={onOpen}/>);await flush();fireEvent.click(screen.getByText('Stop loading'));await flush();fireEvent.click(screen.getByText('Refresh'));await flush();expect(screen.getByText('Open batch review')).toBeTruthy();await act(async()=>resolve({...initial,intent_id:null}));expect(screen.getByText('Open batch review')).toBeTruthy();expect(onOpen).not.toHaveBeenCalled();
});
it('bounds a noncooperative status read to eight seconds',async()=>{
 vi.useFakeTimers();const client={semanticRunIntent:vi.fn().mockReturnValue(new Promise(()=>{}))};render(<SemanticRunLink client={client} tenant={run} run={run} workflow={workflow} onOpen={vi.fn()}/>);await act(async()=>{await vi.advanceTimersByTimeAsync(8000)});expect(screen.getByRole('alert')).toBeTruthy();expect(screen.getByText('Refresh')).toBeTruthy();
});
it('retained old Open handler cannot navigate after background or replacement authority',async()=>{
 const client={semanticRunIntent:vi.fn().mockResolvedValue({...initial,intent_id:intent,state:'completed'})},onOpen=vi.fn();render(<SemanticRunLink client={client} tenant={run} run={run} workflow={workflow} onOpen={onOpen}/>);await flush();const old=renderedPresses.at(-1)!;expect(old).toBeTypeOf('function');act(()=>app.change?.('background'));old();expect(onOpen).not.toHaveBeenCalled();act(()=>app.change?.('active'));await flush();old();expect(onOpen).not.toHaveBeenCalled();fireEvent.click(screen.getByText('Open batch review'));expect(onOpen).toHaveBeenCalledWith(intent);
});
it('retained old Stop cannot cancel replacement observation after Retry',async()=>{
 let resolve!:(v:unknown)=>void;const client={semanticRunIntent:vi.fn().mockReturnValueOnce(new Promise(()=>{})).mockReturnValueOnce(new Promise(done=>{resolve=done}))};render(<SemanticRunLink client={client} tenant={run} run={run} workflow={workflow} onOpen={vi.fn()}/>);await flush();const old=renderedStops.at(-1)!;fireEvent.click(screen.getByText('Stop loading'));fireEvent.click(screen.getByText('Refresh'));await flush();const replacement=client.semanticRunIntent.mock.calls[1][2] as AbortSignal;await act(async()=>old());expect(replacement.aborted).toBe(false);await act(async()=>resolve({...initial,intent_id:intent,state:'completed'}));expect(screen.getByText('Open batch review')).toBeTruthy();
});

vi.mock('react-native-paper',async importOriginal=>{const paper=await importOriginal<typeof import('react-native-paper')>(),React=await import('react');const observe=(props:any)=>{const unwrap=(value:any):any=>typeof value==='string'?value:value?.props?unwrap(value.props.children):value;const label=unwrap(props.children);if(label==='Open batch review')renderedPresses.push(props.onPress);
if(label==='Stop loading')renderedStops.push(props.onPress);};return {...paper,Button:(props:any)=>{observe(props);return React.createElement(paper.Button,props)},TouchableRipple:(props:any)=>{observe(props);return React.createElement(paper.TouchableRipple,props)}};});
