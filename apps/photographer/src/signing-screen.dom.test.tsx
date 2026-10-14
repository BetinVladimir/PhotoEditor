// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {APIError} from '@photoeditor/api-client';
vi.mock('react-native',async()=>import('react-native-web'));
import {SigningScreen} from './signing-screen';
import {SigningAttemptStore} from './signing-attempt-store';

const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const policies={items:[{id:id(4),revision:'1'}]},ticket={run_id:id(5),workflow_id:id(6),state:'queued' as const};
function journal(){const data=new Map<string,string>();return new SigningAttemptStore({getItem:async key=>data.get(key)??null,setItem:async(key,value)=>{data.set(key,value)},removeItem:async key=>{data.delete(key)}},crypto.randomUUID(),id(1),id(2),id(3),async(_origin,action)=>action());}
afterEach(cleanup);
async function submit(){fireEvent.click(await screen.findByRole('radio'));fireEvent.click(screen.getByRole('button',{name:'Sign export'}));}

it('selects only a server policy and keeps the exact journal key after a lost response',async()=>{
 const request=vi.fn().mockResolvedValueOnce(policies).mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce(ticket),onCreated=vi.fn(),store=journal();
 render(<SigningScreen client={{request} as never} tenant={id(2)} journal={store} onCreated={onCreated}/>);
 await submit();fireEvent.click(await screen.findByRole('button',{name:'Retry signing'}));
 await waitFor(()=>expect(onCreated).toHaveBeenCalledWith(ticket));
 expect(request.mock.calls[1][1].body).toEqual({signing_policy_id:id(4)});
 expect(request.mock.calls[2][1].idempotencyKey).toBe(request.mock.calls[1][1].idempotencyKey);
 expect((await store.read())?.ticket).toEqual(ticket);
});

it('opens the saved job without fetching policies or resubmitting',async()=>{
 const store=journal();await store.write({version:1,key:'saved',body:{signing_policy_id:id(4)},ticket});
 const request=vi.fn(),onCreated=vi.fn();render(<SigningScreen client={{request} as never} tenant={id(2)} journal={store} onCreated={onCreated}/>);
 fireEvent.click(await screen.findByRole('button',{name:'Open signing job'}));
 await waitFor(()=>expect(onCreated).toHaveBeenCalledWith(ticket));expect(request).not.toHaveBeenCalled();
});

it('does not offer submission when no server policy is available',async()=>{
 const request=vi.fn().mockResolvedValue({items:[]});render(<SigningScreen client={{request} as never} tenant={id(2)} journal={journal()} onCreated={vi.fn()}/>);
 await screen.findByText('No signing policies are currently available.');
 fireEvent.click(screen.getByRole('button',{name:'Sign export'}));expect(request).toHaveBeenCalledTimes(1);
});

it('blocks retries after export access loss',async()=>{
 const request=vi.fn().mockResolvedValueOnce(policies).mockRejectedValueOnce(new APIError(403,'FORBIDDEN'));
 render(<SigningScreen client={{request} as never} tenant={id(2)} journal={journal()} onCreated={vi.fn()}/>);
 await submit();await screen.findByText('Access to this export is no longer available.');
 expect(screen.queryByRole('button',{name:'Retry signing'})).toBeNull();expect(request).toHaveBeenCalledTimes(2);
});

it('aborts an unmounted submission and ignores its late ticket',async()=>{
 let finish!:(value:unknown)=>void;
 const request=vi.fn().mockResolvedValueOnce(policies).mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})),onCreated=vi.fn();
 const view=render(<SigningScreen client={{request} as never} tenant={id(2)} journal={journal()} onCreated={onCreated}/>);
 await submit();await waitFor(()=>expect(request).toHaveBeenCalledTimes(2));
 const signal=request.mock.calls[1][1].signal as AbortSignal;view.unmount();expect(signal.aborted).toBe(true);
 await act(async()=>finish(ticket));expect(onCreated).not.toHaveBeenCalled();
});

it('rechecks journal authority before opening a previously rendered ticket',async()=>{
 const store=journal();await store.write({version:1,key:'saved',body:{signing_policy_id:id(4)},ticket});
 const request=vi.fn(),onCreated=vi.fn();render(<SigningScreen client={{request} as never} tenant={id(2)} journal={store} onCreated={onCreated}/>);
 await screen.findByRole('button',{name:'Open signing job'});
 vi.spyOn(store,'read').mockRejectedValue(new Error('SIGNING_JOURNAL_EXPIRED'));
 fireEvent.click(screen.getByRole('button',{name:'Open signing job'}));
 await screen.findByText('Access to this export is no longer available.');
 expect(onCreated).not.toHaveBeenCalled();expect(request).not.toHaveBeenCalled();
});

it('removes old policy choices after a failed refresh',async()=>{
 const request=vi.fn().mockResolvedValueOnce(policies).mockRejectedValueOnce(new Error('offline'));
 render(<SigningScreen client={{request} as never} tenant={id(2)} journal={journal()} onCreated={vi.fn()}/>);
 await screen.findByRole('radio');fireEvent.click(screen.getByRole('button',{name:'Refresh policies'}));
 await screen.findByText('Unable to submit signing. Your saved request is retained.');
 expect(screen.queryByRole('radio')).toBeNull();
 fireEvent.click(screen.getByRole('button',{name:'Sign export'}));expect(request).toHaveBeenCalledTimes(2);
});

it('refuses a different policy after restoring a pending attempt',async()=>{
 const store=journal();await store.write({version:1,key:'original',body:{signing_policy_id:id(4)},ticket:null});
 const request=vi.fn().mockResolvedValueOnce({items:[{id:id(7),revision:'2'}]}).mockResolvedValueOnce(ticket),onCreated=vi.fn();
 render(<SigningScreen client={{request} as never} tenant={id(2)} journal={store} onCreated={onCreated}/>);
 fireEvent.click(await screen.findByRole('button',{name:'Retry signing'}));
 await waitFor(()=>expect(onCreated).toHaveBeenCalledWith(ticket));
 expect(screen.queryByRole('radio')).toBeNull();
 expect(request.mock.calls[1][1]).toMatchObject({idempotencyKey:'original',body:{signing_policy_id:id(4)}});
});
