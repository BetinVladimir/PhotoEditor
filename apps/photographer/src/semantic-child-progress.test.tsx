// @vitest-environment jsdom
import React from 'react';
import {AppState,type AppStateStatus} from 'react-native';
import type {SemanticChildStatus} from '@photoeditor/api-client';
import {render,screen,act,cleanup,fireEvent} from '@testing-library/react';
import {afterEach,it,expect,vi} from 'vitest';
vi.mock('react-native',async()=>import('react-native-web'));
import {SemanticChildProgress} from './semantic-child-progress';
const id='11111111-1111-4111-8111-111111111111';
const status={id,kind:'apply' as const,status:'running' as const,revision:'1',counts:{total:1,succeeded:0,failed:0,skipped:0,review:0},stage:'running' as const,progress:0,errors:[]};
afterEach(()=>{cleanup();vi.restoreAllMocks();vi.useRealTimers()});
it('does not start another read while the previous request is pending',async()=>{
 vi.useFakeTimers();let resolve!:(value:typeof status)=>void;
 const semanticChildStatus=vi.fn().mockImplementation(()=>new Promise<typeof status>(done=>{resolve=done}));
 const view=render(<SemanticChildProgress client={{semanticChildStatus}} tenant={id} run={id} onTerminal={()=>{}}/>);
 await act(async()=>{vi.advanceTimersByTime(7999)});
 expect(semanticChildStatus).toHaveBeenCalledOnce();
 await act(async()=>{resolve(status)});
 await act(async()=>{vi.advanceTimersByTime(2999)});
 expect(semanticChildStatus).toHaveBeenCalledOnce();
 await act(async()=>{vi.advanceTimersByTime(1)});
 expect(semanticChildStatus).toHaveBeenCalledTimes(2);
 view.unmount();
});
it('polls without overlap and stops after terminal status',async()=>{
 vi.useFakeTimers();const semanticChildStatus=vi.fn().mockResolvedValueOnce(status).mockResolvedValueOnce({...status,status:'completed',stage:'completed',progress:1,counts:{...status.counts,succeeded:1}}),onTerminal=vi.fn();
 await act(async()=>{render(<SemanticChildProgress client={{semanticChildStatus}} tenant={id} run={id} onTerminal={onTerminal}/>)});
 expect(screen.getByText('Processing: Running · 0%')).toBeTruthy();
 await act(async()=>{vi.advanceTimersByTime(3000)});
 expect(onTerminal).toHaveBeenCalledOnce();
 await act(async()=>{vi.advanceTimersByTime(30000)});expect(semanticChildStatus).toHaveBeenCalledTimes(2);
});
it('aborts outstanding reads on unmount',async()=>{
 const semanticChildStatus=vi.fn().mockImplementation(()=>new Promise(()=>{}));
 const view=render(<SemanticChildProgress client={{semanticChildStatus}} tenant={id} run={id} onTerminal={()=>{}}/>);
 const signal=semanticChildStatus.mock.calls[0][2] as AbortSignal;view.unmount();expect(signal.aborted).toBe(true);
});
it('clears stale progress when access is lost',async()=>{
 vi.useFakeTimers();const semanticChildStatus=vi.fn().mockResolvedValueOnce(status).mockRejectedValueOnce(new Error('DENIED'));
 await act(async()=>{render(<SemanticChildProgress client={{semanticChildStatus}} tenant={id} run={id} onTerminal={()=>{}}/>)});
 await act(async()=>{vi.advanceTimersByTime(3000)});expect(screen.queryByText('Processing: Running · 0%')).toBeNull();expect(screen.getByRole('alert')).toBeTruthy();
});
it('replaces authenticated scope, aborts old IO and ignores its late terminal response',async()=>{
 let resolve!:(value:SemanticChildStatus)=>void;const old={semanticChildStatus:vi.fn().mockImplementation(()=>new Promise(done=>{resolve=done}))},fresh={semanticChildStatus:vi.fn().mockRejectedValue(new Error('DENIED'))},done=vi.fn();
 const view=render(<SemanticChildProgress client={old} tenant={id} run={id} onTerminal={done}/>);
 await act(async()=>{view.rerender(<SemanticChildProgress client={fresh} tenant={id} run={id} onTerminal={done}/>)});
 expect((old.semanticChildStatus.mock.calls[0][2] as AbortSignal).aborted).toBe(true);
 await act(async()=>{resolve({...status,status:'completed',stage:'completed',progress:1})});
 expect(screen.queryByText('Processing: Completed · 100%')).toBeNull();expect(screen.getByRole('alert')).toBeTruthy();expect(done).not.toHaveBeenCalled();
});
it('uses the latest terminal callback without restarting shared polling',async()=>{
 vi.useFakeTimers();const client={semanticChildStatus:vi.fn().mockResolvedValueOnce(status).mockResolvedValueOnce({...status,status:'completed',stage:'completed',progress:1})},old=vi.fn(),fresh=vi.fn();let view!:ReturnType<typeof render>;
 await act(async()=>{view=render(<SemanticChildProgress client={client} tenant={id} run={id} onTerminal={old}/>)});
 view.rerender(<SemanticChildProgress client={client} tenant={id} run={id} onTerminal={fresh}/>);
 expect(client.semanticChildStatus).toHaveBeenCalledTimes(1);
 await act(async()=>{await vi.advanceTimersByTimeAsync(3000)});
 expect(old).not.toHaveBeenCalled();expect(fresh).toHaveBeenCalledOnce();expect(client.semanticChildStatus).toHaveBeenCalledTimes(2);
});

it('suspends shared polling in background and revalidates after foreground return',async()=>{
 vi.useFakeTimers();let change!:(state:AppStateStatus)=>void;const remove=vi.fn();
 vi.spyOn(AppState,'addEventListener').mockImplementation((_event,listener)=>{change=listener;return {remove};});
 const client={semanticChildStatus:vi.fn().mockResolvedValue(status)},done=vi.fn();let view!:ReturnType<typeof render>;
 await act(async()=>{view=render(<SemanticChildProgress client={client} tenant={id} run={id} onTerminal={done}/>)});
 expect(screen.getByText('Processing: Running · 0%')).toBeTruthy();
 act(()=>change('background'));expect(screen.queryByText('Processing: Running · 0%')).toBeNull();
 // The completed read detached its signal; background must cancel the next poll.
 await act(async()=>{await vi.advanceTimersByTimeAsync(30000)});expect(client.semanticChildStatus).toHaveBeenCalledTimes(1);
 await act(async()=>change('active'));expect(client.semanticChildStatus).toHaveBeenCalledTimes(2);expect(screen.getByText('Processing: Running · 0%')).toBeTruthy();
 act(()=>change('active'));expect(client.semanticChildStatus).toHaveBeenCalledTimes(2);
 view.unmount();expect(remove).toHaveBeenCalledOnce();expect(vi.getTimerCount()).toBe(0);
});

it('offers an explicit retry after failure and resumes shared progress',async()=>{
 const client={semanticChildStatus:vi.fn().mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({...status,status:'completed',stage:'completed',progress:1})},done=vi.fn();
 await act(async()=>{render(<SemanticChildProgress client={client} tenant={id} run={id} onTerminal={done}/>)});
 expect(screen.getByRole('alert')).toBeTruthy();expect(client.semanticChildStatus).toHaveBeenCalledOnce();
 await act(async()=>fireEvent.click(screen.getByRole('button',{name:'Retry'})));
 expect(screen.queryByRole('alert')).toBeNull();expect(screen.queryByRole('button',{name:'Retry'})).toBeNull();
 expect(screen.getByText('Processing: Completed · 100%')).toBeTruthy();expect(done).toHaveBeenCalledOnce();expect(client.semanticChildStatus).toHaveBeenCalledTimes(2);
});
