// @vitest-environment jsdom
import React from 'react';
import {APIError} from '@photoeditor/api-client';
import {AppState,type AppStateStatus} from 'react-native';
import {render,screen,fireEvent,waitFor,act,cleanup} from '@testing-library/react';
import {afterEach,it,expect,vi} from 'vitest';
const app=vi.hoisted(()=>({state:'active',handlers:[] as Array<{name:string;invoke:()=>void}>}));
vi.mock('react-native',async()=>{const web=await import('react-native-web'),React=await import('react');return {...web,AppState:{get currentState(){return app.state},addEventListener:(_event:string,_listener:(state:AppStateStatus)=>void)=>({remove:()=>{}})}};});
import {SemanticShownVersionAction} from './semantic-shown-version-action';
const id='11111111-1111-4111-8111-111111111111',version='22222222-2222-4222-8222-222222222222';
afterEach(()=>{cleanup();vi.restoreAllMocks();app.state='active';app.handlers=[];vi.useRealTimers()});
it('opens the materialized version rather than the source target',async()=>{
 const materializeSemanticShownVersion=vi.fn().mockResolvedValue({asset_version_id:version,asset_id:id}),onOpen=vi.fn();
 render(<SemanticShownVersionAction client={{materializeSemanticShownVersion}} tenant={id} target={id} onOpen={onOpen}/>);
 fireEvent.click(screen.getByRole('button'));await waitFor(()=>expect(onOpen).toHaveBeenCalledWith(version,id));
 expect(materializeSemanticShownVersion.mock.calls[0].slice(0,2)).toEqual([id,id]);
});
it('aborts pending mutation and ignores late navigation after unmount',async()=>{
 let resolve!:(result:{asset_version_id:string;asset_id:string})=>void;
 const materializeSemanticShownVersion=vi.fn().mockImplementation(()=>new Promise(done=>{resolve=done})),onOpen=vi.fn();
 const view=render(<SemanticShownVersionAction client={{materializeSemanticShownVersion}} tenant={id} target={id} onOpen={onOpen}/>);
 fireEvent.click(screen.getByRole('button'));await waitFor(()=>expect(materializeSemanticShownVersion).toHaveBeenCalledOnce());
 const signal=materializeSemanticShownVersion.mock.calls[0][2] as AbortSignal;view.unmount();await act(async()=>resolve({asset_version_id:version,asset_id:id}));
 expect(signal.aborted).toBe(true);expect(onOpen).not.toHaveBeenCalled();
});
it('allows retry after uncertain response without opening a version',async()=>{
 const materializeSemanticShownVersion=vi.fn().mockRejectedValueOnce(new Error('lost')).mockResolvedValueOnce({asset_version_id:version,asset_id:id}),onOpen=vi.fn();
 render(<SemanticShownVersionAction client={{materializeSemanticShownVersion}} tenant={id} target={id} onOpen={onOpen}/>);
 fireEvent.click(screen.getByRole('button'));await screen.findByRole('alert');expect(onOpen).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole('button'));await waitFor(()=>expect(onOpen).toHaveBeenCalledOnce());expect(materializeSemanticShownVersion).toHaveBeenCalledTimes(2);
});
it('ignores the old target response after changing scope',async()=>{
 let resolve!:(result:{asset_version_id:string;asset_id:string})=>void;
 const materializeSemanticShownVersion=vi.fn().mockImplementationOnce(()=>new Promise(done=>{resolve=done})).mockResolvedValue({asset_version_id:version,asset_id:id}),onOpen=vi.fn();
 const client={materializeSemanticShownVersion};
 const view=render(<SemanticShownVersionAction client={client} tenant={id} target={id} onOpen={onOpen}/>);
 fireEvent.click(screen.getByRole('button'));await waitFor(()=>expect(materializeSemanticShownVersion).toHaveBeenCalledOnce());
 const oldSignal=materializeSemanticShownVersion.mock.calls[0][2] as AbortSignal;
 view.rerender(<SemanticShownVersionAction client={client} tenant={id} target={version} onOpen={onOpen}/>);
 expect(oldSignal.aborted).toBe(true);
 await act(async()=>resolve({asset_version_id:id,asset_id:id}));expect(onOpen).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole('button'));await waitFor(()=>expect(onOpen).toHaveBeenCalledWith(version,id));
 expect(materializeSemanticShownVersion.mock.calls[1].slice(0,2)).toEqual([id,version]);
});
it('deduplicates clicks while materialization is pending',async()=>{
 let resolve!:(result:{asset_version_id:string;asset_id:string})=>void;
 const materializeSemanticShownVersion=vi.fn().mockImplementation(()=>new Promise(done=>{resolve=done})),onOpen=vi.fn();
 render(<SemanticShownVersionAction client={{materializeSemanticShownVersion}} tenant={id} target={id} onOpen={onOpen}/>);
 const button=screen.getByRole('button');fireEvent.click(button);fireEvent.click(button);
 expect(materializeSemanticShownVersion).toHaveBeenCalledOnce();
 await act(async()=>resolve({asset_version_id:version,asset_id:id}));await waitFor(()=>expect(onOpen).toHaveBeenCalledOnce());
});


it('suppresses a late background result and lets foreground retry open the accepted version',async()=>{
 const listeners=new Set<(state:AppStateStatus)=>void>();vi.spyOn(AppState,'addEventListener').mockImplementation((_event,listener)=>{listeners.add(listener);return {remove:()=>listeners.delete(listener)}});
 let finish!:(value:{asset_version_id:string;asset_id:string})=>void;
 const materializeSemanticShownVersion=vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValue({asset_version_id:version,asset_id:id}),onOpen=vi.fn();
 render(<SemanticShownVersionAction client={{materializeSemanticShownVersion}} tenant={id} target={id} onOpen={onOpen}/>);
 fireEvent.click(screen.getByRole('button'));const oldSignal=materializeSemanticShownVersion.mock.calls[0][2] as AbortSignal;
 act(()=>listeners.forEach(listener=>listener('background')));expect(oldSignal.aborted).toBe(true);expect(screen.getByRole('button').hasAttribute('disabled')).toBe(true);
 act(()=>listeners.forEach(listener=>listener('active')));await waitFor(()=>expect(screen.getByRole('button').hasAttribute('disabled')).toBe(false));
 fireEvent.click(screen.getByRole('button'));await waitFor(()=>expect(onOpen).toHaveBeenCalledWith(version,id));
 await act(async()=>finish({asset_version_id:id,asset_id:version}));expect(onOpen).toHaveBeenCalledOnce();
 expect(materializeSemanticShownVersion.mock.calls.map(call=>call.slice(0,2))).toEqual([[id,id],[id,id]]);
});

it.each(['tenant','client'] as const)('ignores late materialization after changing %s',async(scope)=>{
 let finish!:(value:{asset_version_id:string;asset_id:string})=>void;
 const oldClient={materializeSemanticShownVersion:vi.fn().mockImplementation(()=>new Promise(resolve=>{finish=resolve}))},nextClient={materializeSemanticShownVersion:vi.fn().mockResolvedValue({asset_version_id:version,asset_id:id})},onOpen=vi.fn();
 const view=render(<SemanticShownVersionAction client={oldClient} tenant={id} target={id} onOpen={onOpen}/>);
 fireEvent.click(screen.getByRole('button'));const signal=oldClient.materializeSemanticShownVersion.mock.calls[0][2] as AbortSignal;
 view.rerender(<SemanticShownVersionAction client={scope==='client'?nextClient:oldClient} tenant={scope==='tenant'?version:id} target={id} onOpen={onOpen}/>);
 expect(signal.aborted).toBe(true);await act(async()=>finish({asset_version_id:version,asset_id:id}));expect(onOpen).not.toHaveBeenCalled();
});


it('never materializes or navigates while initially mounted in background',async()=>{
 app.state='background';const listeners=new Set<(state:AppStateStatus)=>void>();vi.spyOn(AppState,'addEventListener').mockImplementation((_event,listener)=>{listeners.add(listener);return {remove:()=>listeners.delete(listener)}});
 const materializeSemanticShownVersion=vi.fn().mockResolvedValue({asset_version_id:version,asset_id:id}),onOpen=vi.fn();
 render(<SemanticShownVersionAction client={{materializeSemanticShownVersion}} tenant={id} target={id} onOpen={onOpen}/>);
 expect(screen.getByRole('button').hasAttribute('disabled')).toBe(true);fireEvent.click(screen.getByRole('button'));await act(async()=>{});
 expect(materializeSemanticShownVersion).not.toHaveBeenCalled();expect(onOpen).not.toHaveBeenCalled();
 act(()=>listeners.forEach(listener=>listener('active')));await waitFor(()=>expect(screen.getByRole('button').hasAttribute('disabled')).toBe(false));
 fireEvent.click(screen.getByRole('button'));await waitFor(()=>expect(onOpen).toHaveBeenCalledWith(version,id));
});

it.each([401,403,404,410])('withdraws result navigation and blocks repeated materialization after HTTP%s',async(status)=>{
 const materializeSemanticShownVersion=vi.fn().mockRejectedValue(new APIError(status,'DENIED')),onOpen=vi.fn(),onAccessLost=vi.fn();
 render(<SemanticShownVersionAction client={{materializeSemanticShownVersion}} tenant={id} target={id} onOpen={onOpen} onAccessLost={onAccessLost}/>);
 fireEvent.click(screen.getByRole('button'));await screen.findByRole('alert');
 await waitFor(()=>expect(screen.getByRole('button').hasAttribute('disabled')).toBe(true));
 fireEvent.click(screen.getByRole('button'));expect(materializeSemanticShownVersion).toHaveBeenCalledTimes(1);expect(onAccessLost).toHaveBeenCalledOnce();expect(onOpen).not.toHaveBeenCalled();
});


it('retained shown-version Stop cannot abort replacement; retained Open cannot run after unmount',async()=>{
 const pending:Array<(value:{asset_version_id:string;asset_id:string})=>void>=[];
 const materializeSemanticShownVersion=vi.fn((_tenant:string,_target:string,_signal?:AbortSignal)=>new Promise<{asset_version_id:string;asset_id:string}>(resolve=>pending.push(resolve))),onOpen=vi.fn();
 const view=render(<SemanticShownVersionAction client={{materializeSemanticShownVersion}} tenant={id} target={id} onOpen={onOpen}/>);
 const oldOpen=app.handlers.at(-1)!.invoke;fireEvent.click(screen.getByRole('button'));await waitFor(()=>expect(pending).toHaveLength(1));
 const oldStop=app.handlers.at(-1)!.invoke;await act(async()=>{oldStop();});
 fireEvent.click(screen.getByRole('button'));await waitFor(()=>expect(pending).toHaveLength(2));
 const signal=materializeSemanticShownVersion.mock.calls[1][2] as AbortSignal;
 await act(async()=>{oldStop();pending[0]({asset_version_id:version,asset_id:id});});expect(signal.aborted).toBe(false);expect(onOpen).not.toHaveBeenCalled();
 view.unmount();await act(async()=>{oldOpen();pending[1]({asset_version_id:version,asset_id:id});});
 expect(materializeSemanticShownVersion).toHaveBeenCalledTimes(2);expect(onOpen).not.toHaveBeenCalled();
});

vi.mock('react-native-paper',async importOriginal=>{const paper=await importOriginal<typeof import('react-native-paper')>(),React=await import('react');const observe=(props:any)=>{const unwrap=(value:any):any=>typeof value==='string'?value:value?.props?unwrap(value.props.children):value;const label=unwrap(props.children);if(typeof label==='string')app.handlers.push({name:label,invoke:props.onPress});};return {...paper,Button:(props:any)=>{observe(props);return React.createElement(paper.Button,props)},TouchableRipple:(props:any)=>{observe(props);return React.createElement(paper.TouchableRipple,props)}};});
