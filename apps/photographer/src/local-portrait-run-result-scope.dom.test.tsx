// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,render,waitFor} from '@testing-library/react';
import type {OwnerAPI} from './owner-api';
const fixture=vi.hoisted(()=>({handlers:[] as Array<{name:string;invoke:()=>void}>,foreground:{active:true,generation:0}}));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>fixture.foreground}));
vi.mock('react-native',async()=>{
 const native=await import('react-native-web'),React=await import('react');
 return {...native};
});
import {LocalPortraitRunResult} from './local-portrait-run-result';
afterEach(()=>{cleanup();fixture.handlers=[];fixture.foreground={active:true,generation:0};});
const api=()=>({me:vi.fn().mockResolvedValue({id:'actor'}),localPortraitResult:vi.fn().mockResolvedValue({result_id:'result'}),applyLocalPortraitResult:vi.fn().mockResolvedValue({asset_version_id:'version'})}) as unknown as OwnerAPI;
function save(){return fixture.handlers.find(item=>item.name==='Save result and open editor')!.invoke;}
it('refuses a retained old save after scope replacement, but permits the new action',async()=>{
 const service=api(),open=vi.fn();
 const view=render(<LocalPortraitRunResult api={service} workflow="first" run="first-run" onOpen={open}/>);
 const old=save();fixture.handlers=[];
 view.rerender(<LocalPortraitRunResult api={service} workflow="second" run="second-run" onOpen={open}/>);
 await act(async()=>{old();});expect(service.me).not.toHaveBeenCalled();
 await act(async()=>{save()();});
 expect(service.localPortraitResult).toHaveBeenCalledWith('second','second-run',expect.any(AbortSignal));
 expect(open).toHaveBeenCalledWith({asset_version_id:'version'});
});
it('refuses a retained save after unmount',async()=>{
 const service=api(),view=render(<LocalPortraitRunResult api={service} workflow="first" run="run" onOpen={vi.fn()}/>);
 const old=save();view.unmount();await act(async()=>{old();});
 expect(service.me).not.toHaveBeenCalled();expect(service.applyLocalPortraitResult).not.toHaveBeenCalled();
});
it('withdraws retained save on background and after a new foreground generation',async()=>{
 const service=api(),open=vi.fn(),props={api:service,workflow:'workflow',run:'run',onOpen:open};
 const view=render(<LocalPortraitRunResult {...props}/>),old=save();
 fixture.foreground={active:false,generation:1};view.rerender(<LocalPortraitRunResult {...props}/>);
 await act(async()=>{old();});expect(service.me).not.toHaveBeenCalled();
 fixture.foreground={active:true,generation:2};fixture.handlers=[];view.rerender(<LocalPortraitRunResult {...props}/>);
 await act(async()=>{old();});expect(service.me).not.toHaveBeenCalled();
 await act(async()=>{save()();});expect(open).toHaveBeenCalledTimes(1);
});

it('a retained Stop cannot abort the replacement save',async()=>{
 const service=api(),open=vi.fn();let complete!:(value:unknown)=>void;
 const pending=new Promise(resolve=>{complete=resolve;});
 vi.mocked(service.applyLocalPortraitResult).mockReturnValue(pending as never);
 render(<LocalPortraitRunResult api={service} workflow="workflow" run="run" onOpen={open}/>);
 await act(async()=>{save()();});
 await waitFor(()=>expect(service.applyLocalPortraitResult).toHaveBeenCalledTimes(1));
 const oldStop=fixture.handlers.filter(item=>item.name==='Stop waiting for saved result').at(-1)!.invoke;
 await act(async()=>{oldStop();});
 await act(async()=>{save()();});
 await waitFor(()=>expect(service.applyLocalPortraitResult).toHaveBeenCalledTimes(2));
 const currentSignal=vi.mocked(service.applyLocalPortraitResult).mock.calls[1][2] as AbortSignal;
 await act(async()=>{oldStop();});expect(currentSignal.aborted).toBe(false);
 await act(async()=>{complete({asset_version_id:'replacement'});});
 expect(open).toHaveBeenCalledTimes(1);expect(open).toHaveBeenCalledWith({asset_version_id:'replacement'});
});

it('aborts an in-flight save on background and recovers only through a fresh foreground action',async()=>{
 const service=api(),open=vi.fn();let release!:(value:unknown)=>void;
 const old=new Promise(resolve=>{release=resolve;});
 vi.mocked(service.applyLocalPortraitResult).mockReturnValueOnce(old as never).mockResolvedValueOnce({asset_version_id:'fresh'} as never);
 const props={api:service,workflow:'workflow',run:'run',onOpen:open};
 const view=render(<LocalPortraitRunResult {...props}/>);
 await act(async()=>{save()();});await waitFor(()=>expect(service.applyLocalPortraitResult).toHaveBeenCalledTimes(1));
 const signal=vi.mocked(service.applyLocalPortraitResult).mock.calls[0][2] as AbortSignal;
 fixture.foreground={active:false,generation:0};view.rerender(<LocalPortraitRunResult {...props}/>);
 expect(signal.aborted).toBe(true);
 await act(async()=>{release({asset_version_id:'old'});});expect(open).not.toHaveBeenCalled();
 fixture.foreground={active:true,generation:1};fixture.handlers=[];view.rerender(<LocalPortraitRunResult {...props}/>);
 await act(async()=>{save()();});expect(open).toHaveBeenCalledTimes(1);expect(open).toHaveBeenCalledWith({asset_version_id:'fresh'});
 expect(service.localPortraitResult).toHaveBeenCalledTimes(2);
});

vi.mock('react-native-paper',async importOriginal=>{const paper=await importOriginal<typeof import('react-native-paper')>(),React=await import('react');const observe=(props:any)=>{const unwrap=(value:any):any=>typeof value==='string'?value:value?.props?unwrap(value.props.children):value;const label=unwrap(props.children);const name=label;
if(typeof name==='string')fixture.handlers.push({name,invoke:props.onPress});};return {...paper,Button:(props:any)=>{observe(props);return React.createElement(paper.Button,props)},TouchableRipple:(props:any)=>{observe(props);return React.createElement(paper.TouchableRipple,props)}};});
