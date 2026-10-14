// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./locale-context',()=>({useLocale:()=>({t:(key:string)=>key})}));
import {InterchangeCreateScreen} from './interchange-create-screen';
import {InterchangeCreateAttemptStore} from './interchange-create-attempt-store';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const body={project_id:id(2),direction:'export' as const,format:'photoeditor_bundle' as const,snapshot_id:id(3)};
const ticket={run_id:id(4),workflow_id:id(5),state:'queued' as const};
function storage(){const data=new Map<string,string>();return {getItem:vi.fn(async(key:string)=>data.get(key)??null),setItem:vi.fn(async(key:string,value:string)=>{data.set(key,value)}),removeItem:vi.fn(async(key:string)=>{data.delete(key)})};}
const journal=(s:ReturnType<typeof storage>)=>new InterchangeCreateAttemptStore(s,'https://photo.test',id(6),id(1),body,async(_origin,action)=>action());
const props=(request:ReturnType<typeof vi.fn>,store:InterchangeCreateAttemptStore,onCreated=vi.fn())=>({client:{request} as never,tenant:id(1),request:body,journal:store,onCreated});
async function create(){await waitFor(()=>expect(screen.getByRole('checkbox').getAttribute('aria-disabled')).not.toBe('true'));fireEvent.click(screen.getByRole('checkbox'));fireEvent.click(screen.getByText('interchange_create_submit'));}
afterEach(cleanup);
it('replays the persisted original request after remount and a lost reply',async()=>{
 const s=storage(),request=vi.fn().mockRejectedValueOnce(new Error('lost reply')).mockResolvedValueOnce(ticket),onCreated=vi.fn();
 const view=render(<InterchangeCreateScreen {...props(request,journal(s),onCreated)}/>);await create();await screen.findByRole('alert');
 const first=request.mock.calls[0][1];view.unmount();render(<InterchangeCreateScreen {...props(request,journal(s),onCreated)}/>);
 fireEvent.click(await screen.findByText('interchange_create_retry'));await screen.findByText('interchange_create_open');
 expect(request.mock.calls[1][1]).toMatchObject({body:first.body,idempotencyKey:first.idempotencyKey});expect(onCreated).not.toHaveBeenCalled();
 expect((await journal(s).read())?.ticket).toEqual(ticket);fireEvent.click(screen.getByText('interchange_create_open'));await waitFor(()=>expect(onCreated).toHaveBeenCalledWith(ticket));
});
it('does not POST if pending intent cannot be persisted',async()=>{
 const s=storage();s.setItem.mockRejectedValue(new Error('disk unavailable'));const request=vi.fn();
 render(<InterchangeCreateScreen {...props(request,journal(s))}/>);await create();await screen.findByRole('alert');expect(request).not.toHaveBeenCalled();
});
it('opens an acknowledged request without another POST',async()=>{
 const s=storage(),store=journal(s);await store.write({version:1,key:id(8),body,ticket});const request=vi.fn(),onCreated=vi.fn();
 render(<InterchangeCreateScreen {...props(request,store,onCreated)}/>);fireEvent.click(await screen.findByText('interchange_create_open'));
 await waitFor(()=>expect(onCreated).toHaveBeenCalledWith(ticket));expect(request).not.toHaveBeenCalled();
});
it('refuses a substituted journal scope before transport',async()=>{
 const s=storage(),request=vi.fn(),store=new InterchangeCreateAttemptStore(s,'https://photo.test',id(6),id(9),body,async(_origin,action)=>action());
 render(<InterchangeCreateScreen {...props(request,store)}/>);await screen.findByRole('alert');expect(s.getItem).not.toHaveBeenCalled();expect(request).not.toHaveBeenCalled();
});

it('does not navigate using an acknowledgement removed after recovery',async()=>{
 const s=storage(),store=journal(s);await store.write({version:1,key:id(8),body,ticket});const request=vi.fn(),onCreated=vi.fn();
 render(<InterchangeCreateScreen {...props(request,store,onCreated)}/>);await screen.findByText('interchange_create_open');
 await store.clear(id(8));fireEvent.click(screen.getByText('interchange_create_open'));await screen.findByRole('alert');
 expect(onCreated).not.toHaveBeenCalled();expect(request).not.toHaveBeenCalled();
});
it('retains the original pending request when acknowledgement persistence fails',async()=>{
 const s=storage(),store=journal(s),request=vi.fn().mockResolvedValue(ticket),onCreated=vi.fn();
 const write=store.write.bind(store);vi.spyOn(store,'write').mockImplementation(async record=>{if(record.ticket)throw new Error('ack disk failure');return write(record);});
 render(<InterchangeCreateScreen {...props(request,store,onCreated)}/>);await create();await screen.findByRole('alert');
 expect((await store.read())?.ticket).toBeNull();expect(screen.queryByText('interchange_create_open')).toBeNull();expect(onCreated).not.toHaveBeenCalled();
 fireEvent.click(screen.getByText('interchange_create_retry'));await waitFor(()=>expect(request).toHaveBeenCalledTimes(2));
 expect(request.mock.calls[1][1].idempotencyKey).toBe(request.mock.calls[0][1].idempotencyKey);
});


it('stops waiting and retries the original persisted key without losing pending intent',async()=>{
 const s=storage(),store=journal(s);let requestSignal:AbortSignal|undefined;
 const request=vi.fn().mockImplementationOnce(async(_path:string,options:{signal:AbortSignal})=>{
  requestSignal=options.signal;
  return await new Promise((_resolve,reject)=>options.signal.addEventListener('abort',()=>reject(new DOMException('Aborted','AbortError')),{once:true}));
 }).mockResolvedValueOnce(ticket);
 render(<InterchangeCreateScreen {...props(request,store)}/>);await create();
 await waitFor(()=>expect(requestSignal).toBeDefined());
 const pending=await store.read();expect(pending?.ticket).toBeNull();
 fireEvent.click(screen.getByText('interchange_create_stop'));
 await screen.findByText('interchange_create_retry');expect(requestSignal!.aborted).toBe(true);
 expect(screen.queryByRole('alert')).toBeNull();expect((await store.read())?.key).toBe(pending?.key);
 fireEvent.click(screen.getByText('interchange_create_retry'));await screen.findByText('interchange_create_open');
 expect(request).toHaveBeenCalledTimes(2);
 expect(request.mock.calls[0][1].idempotencyKey).toBe(request.mock.calls[1][1].idempotencyKey);
});


it('cancelled journal recovery can be retried without sending Create',async()=>{
 const s=storage(),store=journal(s),request=vi.fn();
 let release!:(value:null)=>void;
 vi.spyOn(store,'read').mockImplementationOnce(()=>new Promise(resolve=>{release=resolve;}));
 render(<InterchangeCreateScreen {...props(request,store)}/>);
 await screen.findByText('interchange_create_stop');fireEvent.click(screen.getByText('interchange_create_stop'));
 await act(async()=>{release(null);});
 fireEvent.click(await screen.findByText('interchange_create_restore'));
 await waitFor(()=>expect(screen.getByRole('checkbox').getAttribute('aria-disabled')).not.toBe('true'));
 expect(request).not.toHaveBeenCalled();expect(screen.queryByRole('alert')).toBeNull();
});

it('late Create acknowledgement after unmount never persists ticket or navigates',async()=>{
 const s=storage(),store=journal(s),onCreated=vi.fn();
 let resolve!:(value:typeof ticket)=>void;
 const request=vi.fn(()=>new Promise(done=>{resolve=done;}));
 const view=render(<InterchangeCreateScreen {...props(request,store,onCreated)}/>);await create();
 await waitFor(()=>expect(request).toHaveBeenCalledOnce());const pending=await store.read();
 view.unmount();await act(async()=>{resolve(ticket);});
 expect((await store.read())?.key).toBe(pending?.key);expect((await store.read())?.ticket).toBeNull();expect(onCreated).not.toHaveBeenCalled();
});
