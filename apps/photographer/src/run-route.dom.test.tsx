import {APIError} from '@photoeditor/api-client';
// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import type {RunStatus} from './catalog-types';
const mocks=vi.hoisted(()=>({params:{id:'run',tenant:'tenant'} as {id:string;tenant:string;semanticWorkflow?:string},request:vi.fn(),stream:vi.fn(),push:vi.fn(),resolve:vi.fn()}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('expo-router',()=>({useLocalSearchParams:()=>mocks.params,router:{push:mocks.push}}));
const client={request:mocks.request,eventStream:mocks.stream,semanticRunIntent:mocks.resolve};const auth={client:()=>client};
vi.mock('./auth-context',()=>({useAuth:()=>auth}));
import RunScreen from '../app/runs/[id]';
it('opens semantic review using resolved intent instead of workflow route ID',async()=>{
 const workflow='22222222-2222-4222-8222-222222222222',intent='33333333-3333-4333-8333-333333333333';
 mocks.params={id:'run',tenant:'tenant',semanticWorkflow:workflow};streaming();
 mocks.request.mockResolvedValue({...value('2','waiting_review'),kind:'v2'});
 mocks.resolve.mockResolvedValue({run_id:'run',workflow_id:workflow,intent_id:intent,revision:'2',state:'waiting_review'});
 render(<RunScreen/>);fireEvent.click(await screen.findByText('Open batch review'));
 expect(mocks.push).toHaveBeenCalledWith({pathname:'/semantic/[id]',params:{tenant:'tenant',id:intent}});
});
const value=(revision:string,status:string,id='run'):RunStatus=>({id,kind:'bulk',status,revision,stage:status,progress:status==='completed'?1:0,counts:{total:2,succeeded:status==='completed'?2:0,failed:status==='failed'?2:0,skipped:0,review:0},errors:[]});
function streaming(){mocks.stream.mockImplementation((_path,_cursor,_receive,signal)=>new Promise<void>(resolve=>signal.addEventListener('abort',()=>resolve(),{once:true})));}
afterEach(()=>{cleanup();vi.resetAllMocks();mocks.params={id:'run',tenant:'tenant'};});
it('mounts stream refresh and preserves a newer retry response against an older GET',async()=>{
 streaming();mocks.request.mockResolvedValueOnce(value('1','failed'));render(<RunScreen/>);
 await screen.findByText('Retry failed');const receive=mocks.stream.mock.calls[0][2];
 let release!:(run:RunStatus)=>void;mocks.request.mockReturnValueOnce(new Promise(resolve=>release=resolve));
 act(()=>receive({cursor:'2',type:'run.failed',payload:{run_id:'run'}}));
 mocks.request.mockResolvedValueOnce(value('2','running'));fireEvent.click(screen.getByText('Retry failed'));
 await screen.findByText('Cancel');expect(mocks.request.mock.calls[2][1]).toMatchObject({method:'POST',body:{expected_revision:1}});
 await act(async()=>release(value('1','failed')));expect(screen.queryByText('Retry failed')).toBeNull();
 mocks.request.mockResolvedValueOnce(value('3','completed'));act(()=>receive({cursor:'3',type:'run.completed',payload:{run_id:'run'}}));
 await screen.findByText('completed · 100%');expect(screen.queryByText('Cancel')).toBeNull();
});
it('aborts the old stream and ignores a late mutation after changing tenant/run scope',async()=>{
 streaming();mocks.request.mockResolvedValueOnce(value('1','running'));const view=render(<RunScreen/>);
 await screen.findByText('Cancel');const signal=mocks.stream.mock.calls[0][3] as AbortSignal;
 let release!:(run:RunStatus)=>void;mocks.request.mockReturnValueOnce(new Promise(resolve=>release=resolve));fireEvent.click(screen.getByText('Cancel'));
 await waitFor(()=>expect(mocks.request).toHaveBeenCalledTimes(2));
 mocks.params={id:'next',tenant:'other'};mocks.request.mockResolvedValueOnce(value('1','waiting_review','next'));view.rerender(<RunScreen/>);
 await screen.findByText('waiting_review · 0%');expect(signal.aborted).toBe(true);
 await act(async()=>release(value('2','cancelled')));expect(screen.queryByText('cancelled · 0%')).toBeNull();
 expect(mocks.stream.mock.calls[1][0]).toBe('/api/v1/tenants/other/events');
 view.unmount();expect((mocks.stream.mock.calls[1][3] as AbortSignal).aborted).toBe(true);
});

it('clears displayed run metadata and stops monitoring when access is revoked',async()=>{
 streaming();mocks.request.mockResolvedValueOnce(value('1','running'));render(<RunScreen/>);
 await screen.findByText('Cancel');const receive=mocks.stream.mock.calls[0][2],signal=mocks.stream.mock.calls[0][3] as AbortSignal;
 mocks.request.mockRejectedValueOnce(new APIError(403,'FORBIDDEN'));
 act(()=>receive({cursor:'4',type:'run.running',payload:{run_id:'run'}}));
 await screen.findByText('Unable to load run');expect(screen.queryByText('Cancel')).toBeNull();expect(screen.queryByText('bulk')).toBeNull();expect(signal.aborted).toBe(true);
});

it('clears the run when cancellation reports revoked access',async()=>{
 streaming();mocks.request.mockResolvedValueOnce(value('1','running'));render(<RunScreen/>);
 await screen.findByText('Cancel');const signal=mocks.stream.mock.calls[0][3] as AbortSignal;
 mocks.request.mockRejectedValueOnce(new APIError(403,'FORBIDDEN'));fireEvent.click(screen.getByText('Cancel'));
 await screen.findByText('Unable to cancel run');expect(screen.queryByText('bulk')).toBeNull();expect(screen.queryByText('Cancel')).toBeNull();expect(signal.aborted).toBe(true);
});

it('clears displayed metadata on stream access denial before a catalog retry',async()=>{
 let deny!:(error:unknown)=>void;mocks.stream.mockReturnValueOnce(new Promise((_resolve,reject)=>deny=reject));
 mocks.request.mockResolvedValueOnce(value('1','running'));render(<RunScreen/>);
 await screen.findByText('Cancel');const signal=mocks.stream.mock.calls[0][3] as AbortSignal;
 await act(async()=>deny(new APIError(403,'FORBIDDEN')));
 await screen.findByText('Unable to load run');expect(screen.queryByText('bulk')).toBeNull();expect(screen.queryByText('Cancel')).toBeNull();expect(signal.aborted).toBe(true);expect(mocks.request).toHaveBeenCalledOnce();
});
