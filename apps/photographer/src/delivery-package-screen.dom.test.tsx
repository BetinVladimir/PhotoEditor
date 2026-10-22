// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {APIError} from '@photoeditor/api-client';
// This standalone screen fixture explicitly grants current workspace edit authority.
vi.mock('./workspace-access-context',()=>({useWorkspaceAccess:()=>({actor:'00000000-0000-4000-8000-000000000001',canEdit:true,canAdmin:true})}));
vi.mock('react-native',async()=>import('react-native-web'));
const lifecycle=vi.hoisted(()=>({active:true,generation:0}));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>lifecycle}));
import {DeliveryPackageScreen} from './delivery-package-screen';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
function resource(state='ready',published:string|null=null){return {id:id(1),revision:published?'2':'1',document:{artifact_id:id(7),sha256:'a'.repeat(64)},data:{resource_type:'delivery_packages',id:id(1),tenant_id:id(2),project_id:id(3),created_at:'2026-10-10T11:00:00Z',revision:published?'2':'1',updated_at:'2026-10-10T12:00:00Z',workflow_id:id(4),snapshot_id:id(5),name:'Delivery A',policy_artifact_id:id(6),manifest_artifact_id:id(7),state,published_at:published}};}
const props=(request:ReturnType<typeof vi.fn>)=>({client:{request} as never,tenant:id(2),project:id(3),id:id(1)});
afterEach(()=>{cleanup();vi.useRealTimers();lifecycle.active=true;lifecycle.generation=0;});
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
 await act(async()=>{await vi.advanceTimersByTimeAsync(4000);});
 expect(request).toHaveBeenCalledTimes(2);
 await act(async()=>finish(resource()));
 expect(screen.getByText('Publish package')).toBeTruthy();
 await act(async()=>{await vi.advanceTimersByTimeAsync(4000);});
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
 await act(async()=>{await vi.advanceTimersByTimeAsync(4000);});
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
it('does not acknowledge an ambiguous publication from a different revision on refresh',async()=>{
 const foreign=resource('ready','2026-10-10T12:00:00Z');foreign.revision='4';foreign.data.revision='4';
 const request=vi.fn().mockResolvedValueOnce(resource()).mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce(foreign).mockResolvedValueOnce(resource('ready','2026-10-10T12:00:00Z'));
 render(<DeliveryPackageScreen {...props(request)}/>);
 fireEvent.click(await screen.findByText('Publish package'));await screen.findByText('Retry publication');
 fireEvent.click(screen.getByText('Refresh'));
 await waitFor(()=>expect(request).toHaveBeenCalledTimes(3));
 await waitFor(()=>expect(screen.queryByRole('alert')).not.toBeNull());
 expect(screen.queryByText('Published')).toBeNull();
 fireEvent.click(screen.getByText('Refresh'));await screen.findByText('Published');
 expect(request.mock.calls.map(call=>call[1].method)).toEqual(['GET','POST','GET','GET']);
});

it('hides delivery actions in background and refreshes authority on return',async()=>{
 const request=vi.fn().mockResolvedValue(resource()),p=props(request);
 const view=render(<DeliveryPackageScreen {...p}/>);await screen.findByText('Publish package');
 lifecycle.active=false;view.rerender(<DeliveryPackageScreen {...p}/>);
 expect(screen.queryByText('Publish package')).toBeNull();expect(screen.queryByText('Refresh')).toBeNull();
 lifecycle.active=true;lifecycle.generation++;view.rerender(<DeliveryPackageScreen {...p}/>);
 await screen.findByText('Publish package');expect(request).toHaveBeenCalledTimes(2);
});

it('aborts background inspection and ignores its late proof on foreground return',async()=>{
 const wire=resource('review'),entry={id:id(10),ordinal:0,preset_id:id(8),preset_sha:'b'.repeat(64),required:true,export_id:id(11),state:'ready',reason:null};
 let finish!:(value:unknown)=>void;
 const request=vi.fn().mockResolvedValueOnce(wire)
  .mockResolvedValueOnce({package_id:id(1),revision:'1',manifest:wire.document,items:[entry],next:null})
  .mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve}))
  .mockResolvedValueOnce(wire);
 const p=props(request),view=render(<DeliveryPackageScreen {...p}/>);
 fireEvent.click(await screen.findByText('Load package items'));
 fireEvent.click(await screen.findByText('Verify package composition'));
 await waitFor(()=>expect(request).toHaveBeenCalledTimes(3));
 const signal=request.mock.calls[2][1].signal as AbortSignal;
 lifecycle.active=false;view.rerender(<DeliveryPackageScreen {...p}/>);expect(signal.aborted).toBe(true);
 lifecycle.active=true;lifecycle.generation++;view.rerender(<DeliveryPackageScreen {...p}/>);
 await screen.findByText('Load package items');
 await act(async()=>finish({package_id:id(1),revision:'1',manifest:wire.document,slot_count:1,ready_count:1,omitted_count:0}));
 expect(screen.queryByRole('checkbox')).toBeNull();
 expect(screen.queryByText('Package composition matches the frozen manifest.')).toBeNull();
 expect(request).toHaveBeenCalledTimes(4);
});


it('can replay a durable publication when package refresh fails transiently',async()=>{
 const {DeliveryPublishAttemptStore}=await import('./delivery-publish-attempt-store');
 const values=new Map<string,string>(),storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)}};
 const journal=new DeliveryPublishAttemptStore(storage,'delivery-refresh-retry',id(20),id(2),id(3),id(1),async(_origin,action)=>action());
 await journal.write({version:1,key:'original-publication-key',body:{id:id(1),project_id:id(3),expected_revision:'1'}});
 const request=vi.fn().mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce(resource('ready','2026-10-11T12:00:00Z'));
 render(<DeliveryPackageScreen {...props(request)} journal={journal}/>);
 fireEvent.click(await screen.findByText('Retry publication'));
 await screen.findByText('Published');
 expect(request.mock.calls.map(call=>call[1].method)).toEqual(['GET','POST']);
 expect(request.mock.calls[1][1]).toMatchObject({body:{expected_revision:'1'},idempotencyKey:'original-publication-key'});
 await waitFor(async()=>expect(await journal.read()).toBeNull());
});

it('does not offer recovered publication replay after explicit access loss',async()=>{
 const {DeliveryPublishAttemptStore}=await import('./delivery-publish-attempt-store');
 const values=new Map<string,string>(),storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)}};
 const journal=new DeliveryPublishAttemptStore(storage,'delivery-refresh-forbidden',id(20),id(2),id(3),id(1),async(_origin,action)=>action());
 await journal.write({version:1,key:'preserved-publication-key',body:{id:id(1),project_id:id(3),expected_revision:'1'}});
 const request=vi.fn().mockRejectedValueOnce(new APIError(403,'FORBIDDEN','forbidden'));
 render(<DeliveryPackageScreen {...props(request)} journal={journal}/>);
 await waitFor(()=>expect(screen.queryByRole('alert')).not.toBeNull());
 expect(screen.queryByText('Retry publication')).toBeNull();
 expect(request).toHaveBeenCalledTimes(1);
 expect((await journal.read())?.key).toBe('preserved-publication-key');
});


it('opens task controls only after matching the package workflow',async()=>{
 const onRun=vi.fn(),request=vi.fn().mockResolvedValueOnce(resource()).mockResolvedValueOnce({workflow_id:id(4),run_id:id(20)});
 render(<DeliveryPackageScreen {...props(request)} onRun={onRun}/>);
 fireEvent.click(await screen.findByText('Task progress and controls'));
 await waitFor(()=>expect(onRun).toHaveBeenCalledWith(id(20)));
 expect(request.mock.calls[1][0]).toContain(`/v2-workflows/${id(4)}`);
});
it('does not navigate to a substituted workflow run',async()=>{
 const onRun=vi.fn(),request=vi.fn().mockResolvedValueOnce(resource()).mockResolvedValueOnce({workflow_id:id(99),run_id:id(20)});
 render(<DeliveryPackageScreen {...props(request)} onRun={onRun}/>);
 fireEvent.click(await screen.findByText('Task progress and controls'));
 await screen.findByRole('alert');
 expect(onRun).not.toHaveBeenCalled();
});


it('aborts task navigation in background and ignores its late workflow response',async()=>{
 let resolve!: (value:unknown)=>void;
 const pending=new Promise(resolvePromise=>{resolve=resolvePromise});
 const onRun=vi.fn(),request=vi.fn().mockResolvedValueOnce(resource()).mockReturnValueOnce(pending);
 const p={...props(request),onRun};
 const view=render(<DeliveryPackageScreen {...p}/>);
 fireEvent.click(await screen.findByText('Task progress and controls'));
 await waitFor(()=>expect(request).toHaveBeenCalledTimes(2));
 const signal=request.mock.calls[1][1].signal as AbortSignal;
 lifecycle.active=false;view.rerender(<DeliveryPackageScreen {...p}/>);
 expect(signal.aborted).toBe(true);
 await act(async()=>{resolve({workflow_id:id(4),run_id:id(20)});await pending});
 expect(onRun).not.toHaveBeenCalled();
});
it('clears package authority when task navigation loses access',async()=>{
 const onRun=vi.fn(),request=vi.fn().mockResolvedValueOnce(resource()).mockRejectedValueOnce(new APIError(403,'FORBIDDEN'));
 render(<DeliveryPackageScreen {...props(request)} onRun={onRun}/>);
 fireEvent.click(await screen.findByText('Task progress and controls'));
 await screen.findByRole('alert');
 expect(screen.queryByText('Publish package')).toBeNull();
 expect(onRun).not.toHaveBeenCalled();
});

it('cancels publication waiting and replays the exact frozen CAS while ignoring a late receipt',async()=>{
 let finish!:(value:unknown)=>void;
 const request=vi.fn().mockResolvedValueOnce(resource()).mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValueOnce(resource('ready','2026-10-10T12:00:00Z'));
 render(<DeliveryPackageScreen {...props(request)}/>);fireEvent.click(await screen.findByText('Publish package'));await waitFor(()=>expect(request).toHaveBeenCalledTimes(2));
 const original=request.mock.calls[1][1];fireEvent.click(screen.getByText('Stop loading'));
 await act(async()=>finish(resource('ready','2026-10-10T12:00:00Z')));expect(screen.queryByText('Published')).toBeNull();
 fireEvent.click(screen.getByText('Retry publication'));await screen.findByText('Published');
 expect(request.mock.calls[2][1].body).toEqual(original.body);expect(request.mock.calls[2][1].idempotencyKey).toBe(original.idempotencyKey);
});
it('bounds a stalled initial package read and permits readonly refresh without a mutation',async()=>{
 vi.useFakeTimers();const request=vi.fn().mockImplementationOnce(()=>new Promise(()=>{})).mockResolvedValueOnce(resource());render(<DeliveryPackageScreen {...props(request)}/>);
 await act(async()=>{await vi.advanceTimersByTimeAsync(8000)});expect(screen.getByRole('alert')).toBeTruthy();
 vi.useRealTimers();fireEvent.click(screen.getByText('Refresh'));await screen.findByText('Delivery A');
 expect(request.mock.calls.every(call=>call[1].method==='GET')).toBe(true);
});
