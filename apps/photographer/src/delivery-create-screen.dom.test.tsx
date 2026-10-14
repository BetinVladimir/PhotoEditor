// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {APIError} from '@photoeditor/api-client';
vi.mock('react-native',async()=>import('react-native-web'));
import {DeliveryCreateScreen} from './delivery-create-screen';
import type {DeliveryCreateRequest} from './delivery-create';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const preset={id:id(4),name:'Print',revision:'1',config:{format:'jpeg'},config_hash:'a'.repeat(64),created_at:'2026-10-10T00:00:00Z'};
const page={items:[preset],next_cursor:null};
const ticket={run_id:id(5),workflow_id:id(6),state:'queued'};
const props=(request:ReturnType<typeof vi.fn>,onCreated=vi.fn())=>({client:{request} as never,tenant:id(1),project:id(2),snapshot:id(3),onCreated});
afterEach(cleanup);
async function fill(){await screen.findByText('Print');fireEvent.change(screen.getByLabelText('Delivery name'),{target:{value:'Client delivery'}});fireEvent.click(screen.getByLabelText('Print: Required'));fireEvent.click(screen.getByText('Create delivery'));}
it('retries an identical frozen request after a lost response',async()=>{
 const request=vi.fn().mockResolvedValueOnce(page).mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce(ticket),onCreated=vi.fn();
 render(<DeliveryCreateScreen {...props(request,onCreated)}/>);await fill();
 fireEvent.click(await screen.findByText('Retry creation'));
 await waitFor(()=>expect(onCreated).toHaveBeenCalledWith(ticket));
 expect(request.mock.calls[1][1]).toMatchObject({method:'POST',body:{name:'Client delivery',policy:{required_presets:[preset.id],optional_presets:[]}},idempotencyKey:expect.any(String)});
 expect(request.mock.calls[2][1].body).toEqual(request.mock.calls[1][1].body);
 expect(request.mock.calls[2][1].idempotencyKey).toBe(request.mock.calls[1][1].idempotencyKey);
});
it('clears selections and prevents retry after access loss',async()=>{
 const request=vi.fn().mockResolvedValueOnce(page).mockRejectedValueOnce(new APIError(403,'FORBIDDEN'));
 render(<DeliveryCreateScreen {...props(request)}/>);await fill();
 await waitFor(()=>expect(screen.queryByText('Print')).toBeNull());expect(screen.queryByText('Retry creation')).toBeNull();
 fireEvent.click(screen.getByText('Create delivery'));expect(request).toHaveBeenCalledTimes(2);
});
it('aborts an old scope and ignores its late creation ticket',async()=>{
 let finish!:(value:unknown)=>void;
 const request=vi.fn().mockResolvedValueOnce(page).mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValueOnce(page),onCreated=vi.fn();
 const view=render(<DeliveryCreateScreen {...props(request,onCreated)}/>);await fill();
 await waitFor(()=>expect(request).toHaveBeenCalledTimes(2));const signal=request.mock.calls[1][1].signal as AbortSignal;
 view.rerender(<DeliveryCreateScreen {...props(request,onCreated)} snapshot={id(9)}/>);await waitFor(()=>expect(request).toHaveBeenCalledTimes(3));expect(signal.aborted).toBe(true);
 await act(async()=>finish(ticket));expect(onCreated).not.toHaveBeenCalled();
});
it('recovers preset loading without creating a delivery',async()=>{
 const request=vi.fn().mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(page);
 render(<DeliveryCreateScreen {...props(request)}/>);fireEvent.click(await screen.findByText('Reload presets'));await screen.findByText('Print');
 expect(request).toHaveBeenCalledTimes(2);expect(request.mock.calls.every(call=>call[1].method==='GET')).toBe(true);
});
it('refuses a repeated preset cursor without appending an unbounded page',async()=>{
 const request=vi.fn().mockResolvedValueOnce({items:[preset],next_cursor:'page-2'}).mockResolvedValueOnce({items:[{...preset,id:id(8),name:'Second'}],next_cursor:'page-2'});
 render(<DeliveryCreateScreen {...props(request)}/>);await screen.findByText('Print');
 fireEvent.click(screen.getByText('Load more presets'));
 await screen.findByRole('alert');
 expect(screen.queryByText('Second')).toBeNull();
 expect(request).toHaveBeenCalledTimes(2);
});
it('refuses duplicate preset identities across pages',async()=>{
 const request=vi.fn().mockResolvedValueOnce({items:[preset],next_cursor:'page-2'}).mockResolvedValueOnce({items:[preset],next_cursor:null});
 render(<DeliveryCreateScreen {...props(request)}/>);await screen.findByText('Print');
 fireEvent.click(screen.getByText('Load more presets'));
 await screen.findByRole('alert');
 expect(screen.getAllByText('Print')).toHaveLength(1);
});
it('allows sixteen required presets and refuses a seventeenth selection',async()=>{
 const presets=Array.from({length:17},(_,index)=>({...preset,id:id(index+20),name:`Preset ${index}`}));
 const request=vi.fn().mockResolvedValueOnce({items:presets,next_cursor:null}).mockResolvedValueOnce(ticket);
 render(<DeliveryCreateScreen {...props(request)}/>);await screen.findByText('Preset 0');
 for(let index=0;index<17;index++)fireEvent.click(screen.getByLabelText(`Preset ${index}: Required`));
 fireEvent.change(screen.getByLabelText('Delivery name'),{target:{value:'Sixteen presets'}});
 fireEvent.click(screen.getByText('Create delivery'));
 await waitFor(()=>expect(request).toHaveBeenCalledTimes(2));
 expect(request.mock.calls[1][1].body.policy.required_presets).toEqual(presets.slice(0,16).map(value=>value.id));
});
it('pins a qualified signing policy for required-provenance Delivery creation',async()=>{
 const policy={id:id(9),sha256:'a'.repeat(64),revision:'1'};
 const request=vi.fn().mockResolvedValueOnce(page).mockResolvedValueOnce({items:[policy]}).mockResolvedValueOnce(ticket);
 render(<DeliveryCreateScreen {...props(request)}/>);
 await screen.findByText('Print');
 fireEvent.change(screen.getByLabelText('Delivery name'),{target:{value:'Signed delivery'}});
 fireEvent.click(screen.getByLabelText('Print: Required'));
 fireEvent.click(screen.getByLabelText('Require verified signature'));
 fireEvent.click(await screen.findByLabelText('Signing policy 00000000'));
 fireEvent.click(screen.getByText('Create delivery'));
 await waitFor(()=>expect(request).toHaveBeenCalledTimes(3));
 expect(request.mock.calls[1][0]).toContain(`/projects/${id(2)}/delivery-signing-policies?mime=image%2Fjpeg`);
 expect(request.mock.calls[2][1].body.policy).toMatchObject({provenance:'required',signing_policy_id:policy.id,signing_policy_sha:policy.sha256});
});
it('keeps a selected signing policy when another preset page does not change selected formats',async()=>{
 const policy={id:id(9),sha256:'a'.repeat(64),revision:'1'};
 const second={...preset,id:id(10),name:'Screen'};
 const request=vi.fn().mockResolvedValueOnce({items:[preset],next_cursor:'page-2'}).mockResolvedValueOnce({items:[policy]}).mockResolvedValueOnce({items:[second],next_cursor:null}).mockResolvedValueOnce(ticket);
 render(<DeliveryCreateScreen {...props(request)}/>);
 await screen.findByText('Print');
 fireEvent.change(screen.getByLabelText('Delivery name'),{target:{value:'Signed pages'}});
 fireEvent.click(screen.getByLabelText('Print: Required'));
 fireEvent.click(screen.getByLabelText('Require verified signature'));
 const choice=await screen.findByLabelText('Signing policy 00000000');fireEvent.click(choice);
 fireEvent.click(screen.getByText('Load more presets'));
 await screen.findByText('Screen');
 expect(screen.getByLabelText('Signing policy 00000000').getAttribute('aria-checked')).toBe('true');
 expect(request).toHaveBeenCalledTimes(3);
 fireEvent.click(screen.getByText('Create delivery'));
 await waitFor(()=>expect(request).toHaveBeenCalledTimes(4));
 expect(request.mock.calls[3][1].body.policy).toMatchObject({signing_policy_id:policy.id,signing_policy_sha:policy.sha256});
});
it('keeps the qualified policy when another preset has the same output format',async()=>{
 const policy={id:id(9),sha256:'a'.repeat(64),revision:'1'},second={...preset,id:id(10),name:'Screen'};
 const request=vi.fn().mockResolvedValueOnce({items:[preset,second],next_cursor:null}).mockResolvedValueOnce({items:[policy]}).mockResolvedValueOnce(ticket);
 render(<DeliveryCreateScreen {...props(request)}/>);
 await screen.findByText('Screen');
 fireEvent.change(screen.getByLabelText('Delivery name'),{target:{value:'Two JPEG presets'}});
 fireEvent.click(screen.getByLabelText('Print: Required'));
 fireEvent.click(screen.getByLabelText('Require verified signature'));
 fireEvent.click(await screen.findByLabelText('Signing policy 00000000'));
 fireEvent.click(screen.getByLabelText('Screen: Optional'));
 expect(screen.getByLabelText('Signing policy 00000000').getAttribute('aria-checked')).toBe('true');
 expect(request).toHaveBeenCalledTimes(2);
 fireEvent.click(screen.getByText('Create delivery'));
 await waitFor(()=>expect(request).toHaveBeenCalledTimes(3));
 expect(request.mock.calls[2][1].body.policy).toMatchObject({required_presets:[preset.id],optional_presets:[second.id],signing_policy_id:policy.id,signing_policy_sha:policy.sha256});
});
it('retries a saved signed Delivery with its exact frozen policy without a new catalogue choice',async()=>{
 const saved=structuredClone(savedAttempt()) as unknown as {version:1;key:string;body:DeliveryCreateRequest;ticket:null};saved.body.policy.provenance='required';saved.body.policy.signing_policy_id=id(9);saved.body.policy.signing_policy_sha='b'.repeat(64);
 const journal={read:vi.fn().mockResolvedValue(saved),write:vi.fn().mockResolvedValue(undefined),assertScope:vi.fn()};
 const request=vi.fn().mockResolvedValueOnce(page).mockResolvedValueOnce(ticket);
 render(<DeliveryCreateScreen {...props(request)} snapshot={id(7)} journal={journal as never}/>);
 await screen.findByText('Print');
 fireEvent.click(screen.getByText('Retry creation'));
 await waitFor(()=>expect(request).toHaveBeenCalledTimes(2));
 expect(request.mock.calls.every(call=>!String(call[0]).includes('delivery-signing-policies'))).toBe(true);
 expect(request.mock.calls[1][1].body.policy).toMatchObject({provenance:'required',signing_policy_id:id(9),signing_policy_sha:'b'.repeat(64)});
});
it('does not issue requests for an invalid route scope',async()=>{
 const request=vi.fn();render(<DeliveryCreateScreen {...props(request)} snapshot="invalid"/>);
 await screen.findByRole('alert');expect(request).not.toHaveBeenCalled();
});

const savedAttempt=()=>({version:1 as const,key:id(8),body:{project_id:id(2),snapshot_id:id(7),name:'Saved delivery',policy:{required_presets:[preset.id],optional_presets:[],require_review:true,allow_omitted:false,metadata_policy:'strip' as const,provenance:'off' as const}},ticket:null});
it('blocks creation while journal recovery fails and allows recovery retry',async()=>{
 const request=vi.fn().mockResolvedValue(page),journal={read:vi.fn().mockRejectedValueOnce(new Error('storage unavailable')).mockResolvedValueOnce(null),write:vi.fn()};
 render(<DeliveryCreateScreen {...props(request)} journal={journal as never}/>);
 await screen.findByText('Reload presets');expect(request).not.toHaveBeenCalled();fireEvent.click(screen.getByText('Create delivery'));expect(journal.write).not.toHaveBeenCalled();fireEvent.click(screen.getByText('Reload presets'));await screen.findByText('Print');expect(journal.read).toHaveBeenCalledTimes(2);expect(request).toHaveBeenCalledTimes(1);
});
it('restores the frozen request and uses its original key',async()=>{
 const saved=savedAttempt(),journal={read:vi.fn().mockResolvedValue(saved),write:vi.fn().mockResolvedValue(undefined)},request=vi.fn().mockResolvedValueOnce(page).mockResolvedValueOnce(ticket),onCreated=vi.fn();
 render(<DeliveryCreateScreen {...props(request,onCreated)} snapshot={id(7)} journal={journal as never}/>);
 await screen.findByText('Print');fireEvent.click(screen.getByText('Retry creation'));await waitFor(()=>expect(onCreated).toHaveBeenCalledWith(ticket));
 expect(request.mock.calls[1][1]).toMatchObject({body:saved.body,idempotencyKey:saved.key});expect(journal.write.mock.calls[0][0]).toEqual(saved);expect(journal.write.mock.calls[1][0].ticket).toEqual(ticket);
});
it('opens a stored acknowledgement without submitting another request',async()=>{
 const journal={read:vi.fn().mockResolvedValue({...savedAttempt(),ticket}),write:vi.fn()},request=vi.fn(),onCreated=vi.fn();
 render(<DeliveryCreateScreen {...props(request,onCreated)} snapshot={id(7)} journal={journal as never}/>);
 await waitFor(()=>expect(onCreated).toHaveBeenCalledWith(ticket));expect(request).not.toHaveBeenCalled();expect(journal.write).not.toHaveBeenCalled();
});


it.each(['tenant','project','snapshot'])('rejects another %s journal before ticket recovery or HTTP',async field=>{
 const {DeliveryCreateAttemptStore}=await import('./delivery-create-attempt-store');
 const getItem=vi.fn().mockResolvedValue(null),request=vi.fn(),onCreated=vi.fn();
 const journal=new DeliveryCreateAttemptStore({getItem,setItem:async()=>{},removeItem:async()=>{}},
  'wrong-create-scope',id(20),field==='tenant'?id(90):id(1),field==='project'?id(90):id(2),field==='snapshot'?id(90):id(3),async(_origin,action)=>action());
 render(<DeliveryCreateScreen {...props(request,onCreated)} journal={journal}/>);
 await screen.findByText('Unable to create delivery. Retry the same request.');
 fireEvent.click(screen.getByText('Create delivery'));
 expect(onCreated).not.toHaveBeenCalled();expect(request).not.toHaveBeenCalled();expect(getItem).not.toHaveBeenCalled();
});

it('requires explicit GPS consent and preserves it across retry',async()=>{
 const metadataPreset={...preset,config:{format:'jpeg',metadata_policy:'approved_exif'}};
 const request=vi.fn().mockResolvedValueOnce({items:[metadataPreset],next_cursor:null}).mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(ticket),onCreated=vi.fn();
 render(<DeliveryCreateScreen {...props(request,onCreated)}/>);await screen.findByText('Print');
 fireEvent.click(screen.getByLabelText('Selected EXIF fields'));
 const gps=screen.getByLabelText('GPS location');expect(gps.getAttribute('aria-checked')).toBe('false');
 fireEvent.click(screen.getByLabelText('Author'));fireEvent.click(gps);
 fireEvent.change(screen.getByLabelText('Delivery name'),{target:{value:'Metadata delivery'}});
 fireEvent.click(screen.getByLabelText('Print: Required'));fireEvent.click(screen.getByText('Create delivery'));
 await screen.findByText('Retry creation');expect(request.mock.calls[1][1].body.policy).toMatchObject({metadata_policy:'approved_exif',approved_fields:['author','gps']});
 fireEvent.click(screen.getByLabelText('GPS location'));fireEvent.click(screen.getByText('Retry creation'));
 await waitFor(()=>expect(onCreated).toHaveBeenCalledWith(ticket));expect(request.mock.calls[2][1].body).toEqual(request.mock.calls[1][1].body);
});
it('clears field consent when changing metadata policy',async()=>{
 const request=vi.fn().mockResolvedValue(page);render(<DeliveryCreateScreen {...props(request)}/>);await screen.findByText('Print');
 fireEvent.click(screen.getByLabelText('Selected EXIF fields'));fireEvent.click(screen.getByLabelText('GPS location'));
 fireEvent.click(screen.getByLabelText('Remove metadata'));expect(screen.queryByLabelText('GPS location')).toBeNull();
 fireEvent.click(screen.getByLabelText('Selected EXIF fields'));expect(screen.getByLabelText('GPS location').getAttribute('aria-checked')).toBe('false');
 expect(request).toHaveBeenCalledTimes(1);
});
