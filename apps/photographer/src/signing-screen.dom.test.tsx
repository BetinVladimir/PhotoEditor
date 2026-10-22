// @vitest-environment jsdom
import {expectEnabled,clickWhenEnabled} from './test/ui-readiness';
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {APIError} from '@photoeditor/api-client';
vi.mock('react-native',async()=>import('react-native-web'));
const lifecycle=vi.hoisted(()=>({active:true,generation:0}));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>lifecycle}));
import {SigningScreen} from './signing-screen';
import {SigningAttemptStore} from './signing-attempt-store';

const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const policies={items:[{id:id(4),revision:'1'}]},ticket={run_id:id(5),workflow_id:id(6),state:'queued' as const};
function journal(){const data=new Map<string,string>();return new SigningAttemptStore({getItem:async key=>data.get(key)??null,setItem:async(key,value)=>{data.set(key,value)},removeItem:async key=>{data.delete(key)}},crypto.randomUUID(),id(1),id(2),id(3),async(_origin,action)=>action());}
afterEach(()=>{cleanup();vi.useRealTimers();lifecycle.active=true;lifecycle.generation=0;});
async function submit(){await clickWhenEnabled(await screen.findByRole('radio'));await clickWhenEnabled(screen.getByRole('button',{name:'Sign export'}));}

it('selects only a server policy and keeps the exact journal key after a lost response',async()=>{
 const request=vi.fn().mockResolvedValueOnce(policies).mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce(ticket),onCreated=vi.fn(),store=journal();
 render(<SigningScreen client={{request} as never} tenant={id(2)} journal={store} onCreated={onCreated}/>);
 await submit();const retry=await screen.findByRole('button',{name:'Retry signing'});await waitFor(()=>expectEnabled(retry));await clickWhenEnabled(retry);
 await waitFor(()=>expect(onCreated).toHaveBeenCalledWith(ticket));
 expect(request.mock.calls[1][1].body).toEqual({signing_policy_id:id(4)});
 expect(request.mock.calls[2][1].idempotencyKey).toBe(request.mock.calls[1][1].idempotencyKey);
 expect((await store.read())?.ticket).toEqual(ticket);
});

it('opens the saved job without fetching policies or resubmitting',async()=>{
 const store=journal();await store.write({version:1,key:'saved',body:{signing_policy_id:id(4)},ticket});
 const request=vi.fn(),onCreated=vi.fn();render(<SigningScreen client={{request} as never} tenant={id(2)} journal={store} onCreated={onCreated}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'Open signing job'}));
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
 const request=vi.fn().mockResolvedValueOnce(policies).mockImplementationOnce(()=>new Promise<unknown>(resolve=>{finish=resolve})),onCreated=vi.fn();
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
 await clickWhenEnabled(screen.getByRole('button',{name:'Open signing job'}));
 await screen.findByText('Access to this export is no longer available.');
 expect(onCreated).not.toHaveBeenCalled();expect(request).not.toHaveBeenCalled();
});

it('removes old policy choices after a failed refresh',async()=>{
 const request=vi.fn().mockResolvedValueOnce(policies).mockRejectedValueOnce(new Error('offline'));
 render(<SigningScreen client={{request} as never} tenant={id(2)} journal={journal()} onCreated={vi.fn()}/>);
 await screen.findByRole('radio');await clickWhenEnabled(screen.getByRole('button',{name:'Refresh policies'}));
 await screen.findByText('Unable to submit signing. Your saved request is retained.');
 expect(screen.queryByRole('radio')).toBeNull();
 fireEvent.click(screen.getByRole('button',{name:'Sign export'}));expect(request).toHaveBeenCalledTimes(2);
});

it('refuses a different policy after restoring a pending attempt',async()=>{
 const store=journal();await store.write({version:1,key:'original',body:{signing_policy_id:id(4)},ticket:null});
 const request=vi.fn().mockResolvedValueOnce({items:[{id:id(7),revision:'2'}]}).mockResolvedValueOnce(ticket),onCreated=vi.fn();
 render(<SigningScreen client={{request} as never} tenant={id(2)} journal={store} onCreated={onCreated}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'Retry signing'}));
 await waitFor(()=>expect(onCreated).toHaveBeenCalledWith(ticket));
 expect(screen.queryByRole('radio')).toBeNull();
 expect(request.mock.calls[1][1]).toMatchObject({idempotencyKey:'original',body:{signing_policy_id:id(4)}});
});

it('does not submit a previously selected policy after refresh removes it',async()=>{
 const request=vi.fn().mockResolvedValueOnce(policies).mockResolvedValueOnce({items:[]}),store=journal(),onCreated=vi.fn();
 render(<SigningScreen client={{request} as never} tenant={id(2)} journal={store} onCreated={onCreated}/>);
 await clickWhenEnabled(await screen.findByRole('radio'));await clickWhenEnabled(screen.getByRole('button',{name:'Refresh policies'}));
 await waitFor(()=>expect(screen.queryByRole('radio')).toBeNull());
 const button=await screen.findByRole('button',{name:'Sign export'});expect(button.getAttribute('aria-disabled')).toBe('true');fireEvent.click(button);
 expect(request).toHaveBeenCalledTimes(2);expect(await store.read()).toBeNull();expect(onCreated).not.toHaveBeenCalled();
});

it('cancels a frozen request before the first storage read finishes and retries the identical key',async()=>{
 const store=journal(),base=store.read.bind(store);let finish!:(value:Awaited<ReturnType<typeof store.read>>)=>void;
 const read=vi.spyOn(store,'read').mockImplementationOnce(base).mockImplementationOnce(()=>new Promise<Awaited<ReturnType<typeof store.read>>>(resolve=>{finish=resolve})).mockImplementation(base);
 const request=vi.fn().mockResolvedValueOnce(policies).mockResolvedValueOnce(ticket),onCreated=vi.fn();
 render(<SigningScreen client={{request} as never} tenant={id(2)} journal={store} onCreated={onCreated}/>);await submit();await waitFor(()=>expect(read).toHaveBeenCalledTimes(2));
 await clickWhenEnabled(screen.getByRole('button',{name:'Stop loading'}));await act(async()=>finish(null));expect(request).toHaveBeenCalledTimes(1);expect(onCreated).not.toHaveBeenCalled();
 await clickWhenEnabled(screen.getByRole('button',{name:'Retry signing'}));await waitFor(()=>expect(onCreated).toHaveBeenCalledWith(ticket));expect((await base())?.key).toBe(request.mock.calls[1][1].idempotencyKey);
});
it('cancels an uncertain submission and ignores its late receipt before exact replay',async()=>{
 let finish!:(value:unknown)=>void;const store=journal(),request=vi.fn().mockResolvedValueOnce(policies).mockImplementationOnce(()=>new Promise<unknown>(resolve=>{finish=resolve})).mockResolvedValueOnce(ticket),onCreated=vi.fn();
 render(<SigningScreen client={{request} as never} tenant={id(2)} journal={store} onCreated={onCreated}/>);await submit();await waitFor(()=>expect(request).toHaveBeenCalledTimes(2));const original=request.mock.calls[1][1];
 await clickWhenEnabled(screen.getByRole('button',{name:'Stop loading'}));await act(async()=>finish(ticket));expect(onCreated).not.toHaveBeenCalled();expect((await store.read())?.ticket).toBeNull();
 await clickWhenEnabled(screen.getByRole('button',{name:'Retry signing'}));await waitFor(()=>expect(onCreated).toHaveBeenCalledWith(ticket));expect(request.mock.calls[2][1].idempotencyKey).toBe(original.idempotencyKey);expect(request.mock.calls[2][1].body).toEqual(original.body);
});
it('bounds a stalled journal bootstrap and retries readonly without accepting its late record',async()=>{
 const store=journal(),base=store.read.bind(store);let finish!:(value:Awaited<ReturnType<typeof store.read>>)=>void;vi.spyOn(store,'read').mockImplementationOnce(()=>new Promise<Awaited<ReturnType<typeof store.read>>>(resolve=>{finish=resolve})).mockImplementation(base);
 const request=vi.fn().mockResolvedValue(policies),onCreated=vi.fn();vi.useFakeTimers();render(<SigningScreen client={{request} as never} tenant={id(2)} journal={store} onCreated={onCreated}/>);
 await act(async()=>{await vi.advanceTimersByTimeAsync(8000)});expect(screen.getByRole('button',{name:'Retry loading'})).toBeTruthy();await act(async()=>finish({version:1,key:'late',body:{signing_policy_id:id(4)},ticket}));expect(screen.queryByRole('button',{name:'Open signing job'})).toBeNull();
 vi.useRealTimers();await clickWhenEnabled(screen.getByRole('button',{name:'Retry loading'}));await screen.findByRole('radio');expect(request).toHaveBeenCalledTimes(1);expect(onCreated).not.toHaveBeenCalled();
});
it('releases the whole mutation wait by fifteen seconds even when catch recovery storage hangs',async()=>{
 const store=journal(),base=store.read.bind(store);let late!:(value:Awaited<ReturnType<typeof store.read>>)=>void;
 const read=vi.spyOn(store,'read').mockImplementationOnce(base).mockImplementationOnce(base).mockImplementationOnce(()=>new Promise<Awaited<ReturnType<typeof store.read>>>(resolve=>{late=resolve})).mockImplementation(base);
 const request=vi.fn().mockResolvedValueOnce(policies).mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce(ticket),onCreated=vi.fn();
 render(<SigningScreen client={{request} as never} tenant={id(2)} journal={store} onCreated={onCreated}/>);await screen.findByRole('radio');vi.useFakeTimers();fireEvent.click(screen.getByRole('radio'));fireEvent.click(screen.getByRole('button',{name:'Sign export'}));
 await act(async()=>{await vi.advanceTimersByTimeAsync(15000)});expect(read).toHaveBeenCalledTimes(3);expect(screen.queryByRole('button',{name:'Stop loading'})).toBeNull();
 await act(async()=>late({version:1,key:'wrong-late',body:{signing_policy_id:id(7)},ticket}));vi.useRealTimers();
 await clickWhenEnabled(screen.getByRole('button',{name:'Retry signing'}));await waitFor(()=>expect(onCreated).toHaveBeenCalledWith(ticket));expect(request.mock.calls[2][1].idempotencyKey).toBe(request.mock.calls[1][1].idempotencyKey);expect(request.mock.calls[2][1].body).toEqual(request.mock.calls[1][1].body);
});

it('withdraws foreground authority, refuses the old late receipt and restores the exact durable intent',async()=>{
 const store=journal(),client={request:vi.fn()},onCreated=vi.fn();let finish!:(value:unknown)=>void;
 client.request.mockImplementationOnce(async()=>policies).mockImplementationOnce(()=>new Promise<unknown>(resolve=>{finish=resolve})).mockResolvedValueOnce(policies).mockResolvedValueOnce(ticket);
 const view=render(<SigningScreen client={client as never} tenant={id(2)} journal={store} onCreated={onCreated}/>);await submit();await waitFor(()=>expect(client.request).toHaveBeenCalledTimes(2));const original=client.request.mock.calls[1][1];
 lifecycle.active=false;view.rerender(<SigningScreen client={client as never} tenant={id(2)} journal={store} onCreated={onCreated}/>);expect(screen.queryByRole('button',{name:'Retry signing'})).toBeNull();
 lifecycle.active=true;lifecycle.generation++;view.rerender(<SigningScreen client={client as never} tenant={id(2)} journal={store} onCreated={onCreated}/>);await screen.findByRole('button',{name:'Retry signing'});
 await act(async()=>finish(ticket));expect(onCreated).not.toHaveBeenCalled();await clickWhenEnabled(screen.getByRole('button',{name:'Retry signing'}));await waitFor(()=>expect(onCreated).toHaveBeenCalledWith(ticket));
 expect(client.request.mock.calls[3][1].idempotencyKey).toBe(original.idempotencyKey);expect(client.request.mock.calls[3][1].body).toEqual(original.body);
});
