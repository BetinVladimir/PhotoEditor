// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
import {EditorDelivery} from './editor-delivery';
import type {RenderInputSnapshotRequest} from './render-input-snapshot';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const base={asset_version_id:id(4),source:{artifact_id:id(5),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)};
function setup(){return {v2Binding:vi.fn(async(_asset:string,_signal?:AbortSignal)=>base),createRenderInputSnapshot:vi.fn(async(_project:string,_body:RenderInputSnapshotRequest,_key:string,_signal?:AbortSignal)=>({id:id(6)}))};}
const props=(api:ReturnType<typeof setup>,onPrepared=vi.fn())=>({api:api as never,tenant:id(1),project:id(2),asset:id(3),version:id(4),onPrepared});
afterEach(cleanup);
it('pins the current photo before opening creation',async()=>{
 const api=setup(),onPrepared=vi.fn();render(<EditorDelivery {...props(api,onPrepared)}/>);fireEvent.click(screen.getByText('Create delivery'));
 await waitFor(()=>expect(onPrepared).toHaveBeenCalledWith(id(6)));
 expect(api.createRenderInputSnapshot).toHaveBeenCalledWith(id(2),{sources:[base]},expect.any(String),expect.any(AbortSignal));
});
it('retries the frozen selection and key without rereading latest source',async()=>{
 const api=setup();api.createRenderInputSnapshot.mockRejectedValueOnce(new Error('lost response'));
 render(<EditorDelivery {...props(api)}/>);fireEvent.click(screen.getByText('Create delivery'));fireEvent.click(await screen.findByText('Retry photo selection'));
 await waitFor(()=>expect(api.createRenderInputSnapshot).toHaveBeenCalledTimes(2));expect(api.v2Binding).toHaveBeenCalledTimes(1);
 expect(api.createRenderInputSnapshot.mock.calls[1].slice(0,3)).toEqual(api.createRenderInputSnapshot.mock.calls[0].slice(0,3));
});
it('refuses a changed latest version',async()=>{
 const api=setup();api.v2Binding.mockResolvedValue({...base,asset_version_id:id(9)});
 render(<EditorDelivery {...props(api)}/>);fireEvent.click(screen.getByText('Create delivery'));await screen.findByRole('alert');expect(api.createRenderInputSnapshot).not.toHaveBeenCalled();
});
it('ignores snapshot completion after scope change',async()=>{
 const api=setup(),onPrepared=vi.fn();let finish!:(value:{id:string})=>void;
 api.createRenderInputSnapshot.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve}));
 const view=render(<EditorDelivery {...props(api,onPrepared)}/>);fireEvent.click(screen.getByText('Create delivery'));
 await waitFor(()=>expect(api.createRenderInputSnapshot).toHaveBeenCalledTimes(1));const signal=api.createRenderInputSnapshot.mock.calls[0][3] as AbortSignal;
 view.rerender(<EditorDelivery {...props(api,onPrepared)} version={id(9)}/>);expect(signal.aborted).toBe(true);
 await act(async()=>finish({id:id(6)}));expect(onPrepared).not.toHaveBeenCalled();
});

it('blocks snapshot creation after the editor is disabled during source loading and retains the frozen retry',async()=>{
 const api=setup(),onPrepared=vi.fn();let finish!:(value:typeof base)=>void;
 api.v2Binding.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve}));
 const value=props(api,onPrepared),view=render(<EditorDelivery {...value}/>);
 fireEvent.click(screen.getByText('Create delivery'));await waitFor(()=>expect(api.v2Binding).toHaveBeenCalledTimes(1));
 view.rerender(<EditorDelivery {...value} disabled/>);
 await act(async()=>finish(base));await screen.findByRole('alert');
 expect(api.createRenderInputSnapshot).not.toHaveBeenCalled();expect(onPrepared).not.toHaveBeenCalled();
 view.rerender(<EditorDelivery {...value}/>);
 fireEvent.click(screen.getByText('Retry photo selection'));
 await waitFor(()=>expect(onPrepared).toHaveBeenCalledWith(id(6)));
 expect(api.v2Binding).toHaveBeenCalledTimes(1);
 expect(api.createRenderInputSnapshot.mock.calls[0][1]).toEqual({sources:[base]});
});
