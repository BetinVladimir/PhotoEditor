// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {APIError} from '@photoeditor/api-client';
vi.mock('react-native',async()=>import('react-native-web'));
import {DeliveryPackageScreen} from './delivery-package-screen';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
function resource(state='ready',published:string|null=null){return {id:id(1),revision:published?'2':'1',document:{artifact_id:id(7),sha256:'a'.repeat(64)},data:{resource_type:'delivery_packages',id:id(1),tenant_id:id(2),project_id:id(3),created_at:'2026-10-10T11:00:00Z',revision:published?'2':'1',updated_at:'2026-10-10T12:00:00Z',workflow_id:id(4),snapshot_id:id(5),name:'Delivery A',policy_artifact_id:id(6),manifest_artifact_id:id(7),state,published_at:published}};}
const props=(request:ReturnType<typeof vi.fn>)=>({client:{request} as never,tenant:id(2),project:id(3),id:id(1)});
afterEach(()=>{cleanup();vi.useRealTimers();});
it('publishes a ready package and removes the action after success',async()=>{
 const request=vi.fn().mockResolvedValueOnce(resource()).mockResolvedValueOnce(resource('ready','2026-10-10T12:00:00Z'));
 render(<DeliveryPackageScreen {...props(request)}/>);
 fireEvent.click(await screen.findByText('Publish package'));
 await screen.findByText('Published');expect(screen.queryByText('Publish package')).toBeNull();
 expect(request.mock.calls[1][1]).toMatchObject({method:'POST',body:{expected_revision:'1'},idempotencyKey:expect.any(String)});
});
it('offers manifest and ready-slot downloads only after publication',async()=>{
 const published=resource('ready','2026-10-10T12:00:00Z'),entry={id:id(10),ordinal:0,preset_id:id(8),preset_sha:'b'.repeat(64),required:true,export_id:id(11),state:'ready',reason:null};
 const request=vi.fn().mockResolvedValueOnce(published).mockResolvedValueOnce({package_id:id(1),revision:'2',manifest:published.document,items:[entry],next:null});
 render(<DeliveryPackageScreen {...props(request)}/>);
 await screen.findByText('Download manifest');fireEvent.click(screen.getByText('Load package items'));
 await screen.findByText('Download result');
});
it('does not offer a download for unpublished or review packages',async()=>{
 const request=vi.fn().mockResolvedValueOnce(resource('review'));
 render(<DeliveryPackageScreen {...props(request)}/>);
 await screen.findByText('Awaiting review');
 expect(screen.queryByText('Download manifest')).toBeNull();
 expect(screen.queryByText('Download result')).toBeNull();
});
it('does not offer blind review acceptance or terminal publication',async()=>{
 const request=vi.fn().mockResolvedValue(resource('review'));const view=render(<DeliveryPackageScreen {...props(request)}/>);
 await screen.findByText('Awaiting review');expect(screen.queryByText('Publish package')).toBeNull();
 request.mockResolvedValue(resource('failed'));fireEvent.click(screen.getByText('Refresh'));await screen.findByText('Failed');expect(screen.queryByText('Publish package')).toBeNull();view.unmount();
});
it('retries the same CAS and key after losing the response',async()=>{
 const request=vi.fn().mockResolvedValueOnce(resource()).mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce(resource('ready','2026-10-10T12:00:00Z'));
 render(<DeliveryPackageScreen {...props(request)}/>);fireEvent.click(await screen.findByText('Publish package'));
 fireEvent.click(await screen.findByText('Retry publication'));await screen.findByText('Published');
 expect(request.mock.calls[2][1].idempotencyKey).toBe(request.mock.calls[1][1].idempotencyKey);
 expect(request.mock.calls[2][1].body).toEqual(request.mock.calls[1][1].body);
});
it('refresh reconciles an already published package without another mutation',async()=>{
 const request=vi.fn().mockResolvedValueOnce(resource()).mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce(resource('ready','2026-10-10T12:00:00Z'));
 render(<DeliveryPackageScreen {...props(request)}/>);fireEvent.click(await screen.findByText('Publish package'));await screen.findByText('Retry publication');
 fireEvent.click(screen.getByText('Refresh'));await screen.findByText('Published');expect(screen.queryByText('Retry publication')).toBeNull();
 expect(request.mock.calls.map(call=>call[1].method)).toEqual(['GET','POST','GET']);
});
it('clears metadata after publication access loss',async()=>{
 const request=vi.fn().mockResolvedValueOnce(resource()).mockRejectedValueOnce(new APIError(403,'FORBIDDEN'));
 render(<DeliveryPackageScreen {...props(request)}/>);fireEvent.click(await screen.findByText('Publish package'));
 await screen.findByText('Access to this package is no longer available.');expect(screen.queryByText('Delivery A')).toBeNull();expect(screen.queryByText('Retry publication')).toBeNull();
});
it('aborts old scope and ignores late publication response',async()=>{
 let finish!:(value:unknown)=>void;
 const request=vi.fn().mockResolvedValueOnce(resource()).mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValueOnce({...resource(),id:id(9),data:{...resource().data,id:id(9),name:'Delivery B'}});
 const view=render(<DeliveryPackageScreen {...props(request)}/>);fireEvent.click(await screen.findByText('Publish package'));
 await waitFor(()=>expect(request).toHaveBeenCalledTimes(2));const signal=request.mock.calls[1][1].signal as AbortSignal;
 view.rerender(<DeliveryPackageScreen {...props(request)} id={id(9)}/>);await screen.findByText('Delivery B');expect(signal.aborted).toBe(true);
 await act(async()=>finish(resource('ready','2026-10-10T12:00:00Z')));expect(screen.queryByText('Published')).toBeNull();expect(screen.queryByText('Delivery A')).toBeNull();
});

it('loads the full metadata list and clears it on refresh',async()=>{
 const wire=resource(),item={id:id(10),ordinal:0,preset_id:id(8),preset_sha:'b'.repeat(64),required:true,export_id:id(11),state:'ready',reason:null};
 const request=vi.fn().mockResolvedValueOnce(wire).mockResolvedValueOnce({package_id:id(1),revision:'1',manifest:wire.document,items:[item],next:null}).mockResolvedValueOnce(wire);
 render(<DeliveryPackageScreen {...props(request)}/>);fireEvent.click(await screen.findByText('Load package items'));
 await screen.findByText('Items loaded: 1');expect(screen.getByText(`1 · ${id(8)}`)).toBeTruthy();
 fireEvent.click(screen.getByText('Refresh'));await screen.findByText('Publish package');expect(screen.queryByText('Items loaded: 1')).toBeNull();
});
it('clears package metadata when item inspection loses access',async()=>{
 const request=vi.fn().mockResolvedValueOnce(resource()).mockRejectedValueOnce(new APIError(403,'FORBIDDEN'));
 render(<DeliveryPackageScreen {...props(request)}/>);fireEvent.click(await screen.findByText('Load package items'));
 await screen.findByText('Access to this package is no longer available.');expect(screen.queryByText('Delivery A')).toBeNull();expect(screen.queryByText('Publish package')).toBeNull();
});

it('requires complete inspection and explicit acceptance before publishing review',async()=>{
 const wire=resource('review'),entry={id:id(10),ordinal:0,preset_id:id(8),preset_sha:'b'.repeat(64),required:true,export_id:id(11),state:'ready',reason:null};
 const request=vi.fn().mockResolvedValueOnce(wire)
 .mockResolvedValueOnce({package_id:id(1),revision:'1',manifest:wire.document,items:[entry],next:null})
 .mockResolvedValueOnce({package_id:id(1),revision:'1',manifest:wire.document,slot_count:1,ready_count:1,omitted_count:0})
 .mockResolvedValueOnce(resource('ready','2026-10-10T12:00:00Z'));
 render(<DeliveryPackageScreen {...props(request)}/>);fireEvent.click(await screen.findByText('Load package items'));
 fireEvent.click(await screen.findByText('Verify package composition'));
 await screen.findByText('Package composition matches the frozen manifest.');expect(screen.queryByText('Publish package')).toBeNull();
 fireEvent.click(screen.getByRole('checkbox'));fireEvent.click(await screen.findByText('Publish package'));
 await screen.findByText('Published');expect(request.mock.calls[3][1].body).toEqual({expected_revision:'1'});
});
it('does not allow acceptance after a mismatched inspection proof',async()=>{
 const wire=resource('review'),entry={id:id(10),ordinal:0,preset_id:id(8),preset_sha:'b'.repeat(64),required:true,export_id:id(11),state:'ready',reason:null};
 const request=vi.fn().mockResolvedValueOnce(wire)
 .mockResolvedValueOnce({package_id:id(1),revision:'1',manifest:wire.document,items:[entry],next:null})
 .mockResolvedValueOnce({package_id:id(1),revision:'2',manifest:wire.document,slot_count:1,ready_count:1,omitted_count:0});
 render(<DeliveryPackageScreen {...props(request)}/>);fireEvent.click(await screen.findByText('Load package items'));fireEvent.click(await screen.findByText('Verify package composition'));
 await screen.findByText('The complete package could not be verified. Refresh and inspect it again.');expect(screen.queryByRole('checkbox')).toBeNull();expect(screen.queryByText('Publish package')).toBeNull();
});

it('restores a publication after remount and reuses the durable key and CAS',async()=>{
 const {DeliveryPublishAttemptStore}=await import('./delivery-publish-attempt-store');
 const values=new Map<string,string>(),storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)}};
 const journal=new DeliveryPublishAttemptStore(storage,'origin',id(20),id(2),id(3),id(1),async(_origin,action)=>action());
 const firstRequest=vi.fn().mockResolvedValueOnce(resource()).mockRejectedValueOnce(new Error('network'));
 const first=render(<DeliveryPackageScreen {...props(firstRequest)} journal={journal}/>);
 fireEvent.click(await screen.findByText('Publish package'));await screen.findByText('Retry publication');
 const saved=await journal.read();expect(saved).not.toBeNull();first.unmount();
 const secondRequest=vi.fn().mockResolvedValueOnce(resource()).mockResolvedValueOnce(resource('ready','2026-10-11T12:00:00Z'));
 render(<DeliveryPackageScreen {...props(secondRequest)} journal={journal}/>);
 fireEvent.click(await screen.findByText('Retry publication'));await screen.findByText('Published');
 expect(secondRequest.mock.calls[1][1]).toMatchObject({body:{expected_revision:'1'},idempotencyKey:saved!.key});
 expect(await journal.read()).toBeNull();
});
it('does not send publication when durable intent cannot be saved',async()=>{
 const {DeliveryPublishAttemptStore}=await import('./delivery-publish-attempt-store');
 const storage={getItem:async()=>null,setItem:async()=>{throw new Error('storage unavailable')},removeItem:async()=>{}};
 const journal=new DeliveryPublishAttemptStore(storage,'origin',id(20),id(2),id(3),id(1),async(_origin,action)=>action());
 const request=vi.fn().mockResolvedValueOnce(resource());
 render(<DeliveryPackageScreen {...props(request)} journal={journal}/>);
 fireEvent.click(await screen.findByText('Publish package'));await screen.findByText('Retry publication');
 expect(request).toHaveBeenCalledTimes(1);
});

it('keeps confirmed publication visible when local journal cleanup fails',async()=>{
 const {DeliveryPublishAttemptStore}=await import('./delivery-publish-attempt-store');
 const values=new Map<string,string>();let removable=false;
 const storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{if(!removable)throw new Error('storage unavailable');values.delete(k)}};
 const journal=new DeliveryPublishAttemptStore(storage,'cleanup-origin',id(20),id(2),id(3),id(1),async(_origin,action)=>action());
 const published=resource('ready','2026-10-11T12:00:00Z');
 const request=vi.fn().mockResolvedValueOnce(resource()).mockResolvedValueOnce(published).mockResolvedValueOnce(published);
 render(<DeliveryPackageScreen {...props(request)} journal={journal}/>);
 fireEvent.click(await screen.findByText('Publish package'));await screen.findByText('Published');
 await screen.findByText('Published. The local retry record could not be cleared. Refresh to retry cleanup.');
 expect(screen.queryByText('Retry publication')).toBeNull();expect(screen.queryByText('Publish package')).toBeNull();expect(await journal.read()).not.toBeNull();
 removable=true;fireEvent.click(screen.getByText('Refresh'));await waitFor(async()=>expect(await journal.read()).toBeNull());
 expect(request.mock.calls.map(call=>call[1].method)).toEqual(['GET','POST','GET']);
});
it('shows current published state when recovered-journal cleanup fails on load',async()=>{
 const {DeliveryPublishAttemptStore}=await import('./delivery-publish-attempt-store');
 const values=new Map<string,string>(),storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async()=>{throw new Error('storage unavailable')}};
 const journal=new DeliveryPublishAttemptStore(storage,'recovery-cleanup-origin',id(20),id(2),id(3),id(1),async(_origin,action)=>action());
 await journal.write({version:1,key:'original-key',body:{id:id(1),project_id:id(3),expected_revision:'1'}});
 const request=vi.fn().mockResolvedValueOnce(resource('ready','2026-10-11T12:00:00Z'));
 render(<DeliveryPackageScreen {...props(request)} journal={journal}/>);
 await screen.findByText('Published. The local retry record could not be cleared. Refresh to retry cleanup.');
 expect(screen.getByText('Published')).toBeTruthy();expect(screen.queryByText('Retry publication')).toBeNull();expect(request).toHaveBeenCalledTimes(1);
});


it('polls building serially and stops at ready without automatic publication',async()=>{
 vi.useFakeTimers();
 let finish!:(value:unknown)=>void;
 const request=vi.fn().mockResolvedValueOnce(resource('building'))
  .mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve;}));
 const view=render(<DeliveryPackageScreen {...props(request)}/>);
 await act(async()=>{});
 expect(request).toHaveBeenCalledTimes(1);
 await act(async()=>{await vi.advanceTimersByTimeAsync(2000);});
 expect(request).toHaveBeenCalledTimes(2);
 await act(async()=>{await vi.advanceTimersByTimeAsync(10000);});
 expect(request).toHaveBeenCalledTimes(2);
 await act(async()=>finish(resource()));
 expect(screen.getByText('Publish package')).toBeTruthy();
 await act(async()=>{await vi.advanceTimersByTimeAsync(10000);});
 expect(request).toHaveBeenCalledTimes(2);
 expect(request.mock.calls.every(call=>call[1]?.method!=='POST')).toBe(true);
 view.unmount();
});

it('stops building polling after read error until explicit refresh',async()=>{
 vi.useFakeTimers();
 const request=vi.fn().mockResolvedValueOnce(resource('building')).mockRejectedValueOnce(new Error('offline'));
 render(<DeliveryPackageScreen {...props(request)}/>);
 await act(async()=>{});
 await act(async()=>{await vi.advanceTimersByTimeAsync(2000);});
 expect(request).toHaveBeenCalledTimes(2);
 await act(async()=>{await vi.advanceTimersByTimeAsync(10000);});
 expect(request).toHaveBeenCalledTimes(2);
});

it('cleans building polling timer on unmount',async()=>{
 vi.useFakeTimers();
 const request=vi.fn().mockResolvedValue(resource('building'));
 const view=render(<DeliveryPackageScreen {...props(request)}/>);
 await act(async()=>{});view.unmount();
 await act(async()=>{await vi.advanceTimersByTimeAsync(10000);});
 expect(request).toHaveBeenCalledTimes(1);
});


it.each(['tenant','project','package'])('refuses a journal for another %s before HTTP or storage',async field=>{
 const {DeliveryPublishAttemptStore}=await import('./delivery-publish-attempt-store');
 const getItem=vi.fn().mockResolvedValue(null),request=vi.fn();
 const journal=new DeliveryPublishAttemptStore({getItem,setItem:async()=>{},removeItem:async()=>{}},
  'wrong-journal-origin',id(20),field==='tenant'?id(90):id(2),field==='project'?id(90):id(3),field==='package'?id(90):id(1),async(_origin,action)=>action());
 render(<DeliveryPackageScreen {...props(request)} journal={journal}/>);
 await screen.findByText('Could not load delivery package.');
 expect(request).not.toHaveBeenCalled();expect(getItem).not.toHaveBeenCalled();
 expect(screen.queryByText('Retry publication')).toBeNull();
});
