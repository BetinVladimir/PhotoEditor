// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {APIError} from '@photoeditor/api-client';
import type {OwnerAPI} from './owner-api';
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./local-portrait-run-result',()=>({LocalPortraitRunResult:({run,workflow}:{run:string;workflow:string})=><div>{run}:{workflow}</div>}));
import {LocalPortraitRunDiscovery} from './local-portrait-run-discovery';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
afterEach(cleanup);
it('opens the exact discovered workflow after retrying transient discovery',async()=>{
 const discoverLocalPortraitResult=vi.fn().mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce({run_id:id(1),workflow_id:id(2)});
 render(<LocalPortraitRunDiscovery api={{discoverLocalPortraitResult} as unknown as OwnerAPI} run={id(1)} onOpen={vi.fn()} onAccessDenied={vi.fn()}/>);
 fireEvent.click(await screen.findByText('Retry result discovery'));await screen.findByText(`${id(1)}:${id(2)}`);expect(discoverLocalPortraitResult).toHaveBeenCalledTimes(2);
});
it.each([401,403,404,410])('withdraws discovery after current access denial %s',async(status)=>{
 const onAccessDenied=vi.fn(),discoverLocalPortraitResult=vi.fn().mockRejectedValue(new APIError(status,'DENIED'));
 render(<LocalPortraitRunDiscovery api={{discoverLocalPortraitResult} as unknown as OwnerAPI} run={id(1)} onOpen={vi.fn()} onAccessDenied={onAccessDenied}/>);
 await waitFor(()=>expect(onAccessDenied).toHaveBeenCalledOnce());expect(screen.queryByText('Retry result discovery')).toBeNull();expect(discoverLocalPortraitResult).toHaveBeenCalledOnce();
});
it('does not expose a foreign run or a late workflow from a replaced account/API scope',async()=>{
 let resolve!:(v:unknown)=>void;const old=vi.fn().mockImplementation(()=>new Promise(r=>{resolve=r;}));
 const view=render(<LocalPortraitRunDiscovery api={{discoverLocalPortraitResult:old} as unknown as OwnerAPI} run={id(1)} onOpen={vi.fn()} onAccessDenied={vi.fn()}/>);
 await waitFor(()=>expect(old).toHaveBeenCalledOnce());const signal=old.mock.calls[0][1] as AbortSignal;
 view.rerender(<LocalPortraitRunDiscovery api={{discoverLocalPortraitResult:async()=>({run_id:id(1),workflow_id:null})} as unknown as OwnerAPI} run={id(1)} onOpen={vi.fn()} onAccessDenied={vi.fn()}/>);
 expect(signal.aborted).toBe(true);resolve({run_id:id(1),workflow_id:id(2)});await Promise.resolve();expect(screen.queryByText(`${id(1)}:${id(2)}`)).toBeNull();
});
it('rejects a discovery result bound to a different run',async()=>{
 render(<LocalPortraitRunDiscovery api={{discoverLocalPortraitResult:async()=>({run_id:id(3),workflow_id:id(2)})} as unknown as OwnerAPI} run={id(1)} onOpen={vi.fn()} onAccessDenied={vi.fn()}/>);
 await screen.findByText('Retry result discovery');expect(screen.queryByText(`${id(1)}:${id(2)}`)).toBeNull();
});
it('offers initial Cancel and readonly Retry while rejecting late discovery',async()=>{
 let resolve!:(v:unknown)=>void;const discoverLocalPortraitResult=vi.fn().mockReturnValueOnce(new Promise(done=>{resolve=done})).mockResolvedValue({run_id:id(1),workflow_id:id(2)});const api={discoverLocalPortraitResult} as unknown as OwnerAPI;render(<LocalPortraitRunDiscovery api={api} run={id(1)} onOpen={vi.fn()} onAccessDenied={vi.fn()}/>);fireEvent.click(screen.getByText('Stop loading'));fireEvent.click(screen.getByText('Retry loading'));await screen.findByText(`${id(1)}:${id(2)}`);await act(async()=>resolve({run_id:id(1),workflow_id:id(3)}));expect(screen.getByText(`${id(1)}:${id(2)}`)).toBeTruthy();
});
