// @vitest-environment jsdom
import {clickWhenEnabled} from './test/ui-readiness';
import React from 'react';
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
const renderedStops=vi.hoisted(()=>[] as Array<()=>void>);
vi.mock('react-native',async()=>{const native=await import('react-native-web'),React=await import('react');return{...native}});
import {APIError} from '@photoeditor/api-client';
import {SemanticCreate} from './semantic-create';
import type {SemanticAttemptStore} from './semantic-attempt-store';
import type {Collection} from './catalog-types';
import en from '../../../locales/en/common.json';
import ru from '../../../locales/ru/common.json';
import bg from '../../../locales/bg/common.json';
const locale=vi.hoisted(()=>({value:'en' as 'en'|'ru'|'bg'}));
const catalogs={en,ru,bg};
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:keyof typeof en)=>catalogs[locale.value][key]})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
const id='11111111-1111-4111-8111-111111111111',other='22222222-2222-4222-8222-222222222222';
const binding={asset_version_id:id,source:{artifact_id:other,sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)};
const mask={artifact_id:id,sha256:'c'.repeat(64)};
const collection:Collection={id,tenant_id:id,project_id:id,name:'Portraits',description:'',state:'ready',revision:'9007199254740993',sort_mode:'manual',created_at:'2026-01-01T00:00:00Z'};
const snapshot={id:other,digest:'d'.repeat(64),item_count:2,expires_at:'2099-01-01T00:00:00Z'};
const ticket={run_id:id,workflow_id:other,state:'queued' as const};
function fixture(){return{v2Binding:vi.fn().mockResolvedValue(binding),selectionSnapshot:vi.fn().mockResolvedValue(snapshot),proposeSemanticIntent:vi.fn().mockResolvedValue(ticket)}}
it('does not perform network IO when the original journal cannot be saved',async()=>{
 const api=fixture(),store={write:vi.fn().mockRejectedValue(new Error('disk full'))} as unknown as SemanticAttemptStore;
 render(<SemanticCreate api={api} store={store} tenant={id} asset={id} version={id} mask={mask} collection={collection} onSubmitted={vi.fn()}/>);
 fireEvent.click(screen.getByText('Prepare batch review'));await screen.findByText('Retry batch request');
 expect(api.v2Binding).not.toHaveBeenCalled();expect(api.selectionSnapshot).not.toHaveBeenCalled();expect(api.proposeSemanticIntent).not.toHaveBeenCalled();
});
it('does not repeat accepted admission when navigation throws',async()=>{
 const api=fixture(),onSubmitted=vi.fn().mockImplementationOnce(()=>{throw new Error('navigation unavailable')});
 render(<SemanticCreate api={api} tenant={id} asset={id} version={id} mask={mask} collection={collection} onSubmitted={onSubmitted}/>);
 fireEvent.click(screen.getByText('Prepare batch review'));await screen.findByText('Retry batch request');
 const retry=screen.getByRole('button',{name:'Retry batch request'});await waitFor(()=>expect(retry.hasAttribute('disabled')).toBe(false));fireEvent.click(retry);
 await waitFor(()=>expect(onSubmitted).toHaveBeenCalledTimes(2));expect(api.proposeSemanticIntent).toHaveBeenCalledTimes(1);
});
afterEach(()=>{cleanup();locale.value='en';renderedStops.length=0});
it('keeps immutable intent and proposal key after unavailable/uncertain admission',async()=>{
 const api=fixture(),onSubmitted=vi.fn();api.proposeSemanticIntent.mockRejectedValueOnce(new APIError(503,'DEPENDENCY_UNAVAILABLE')).mockResolvedValueOnce(ticket);
 render(<SemanticCreate api={api} tenant={id} asset={id} version={id} mask={mask} collection={collection} onSubmitted={onSubmitted}/>);
 fireEvent.click(screen.getByText('Prepare batch review'));
 await waitFor(()=>expect(screen.getByText('Retry batch request')).toBeTruthy());
 expect(onSubmitted).not.toHaveBeenCalled();
 const retryButton=screen.getByRole('button',{name:'Retry batch request'});
 await waitFor(()=>expect(retryButton.hasAttribute('disabled')).toBe(false));
 fireEvent.click(retryButton);
 await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(ticket));
 expect(api.selectionSnapshot).toHaveBeenCalledTimes(1);
 expect(api.proposeSemanticIntent.mock.calls[0].slice(0,2)).toEqual(api.proposeSemanticIntent.mock.calls[1].slice(0,2));
 expect(api.proposeSemanticIntent.mock.calls[0][0]).toEqual({schema_version:1,kind:'local_exposure',reference:binding,reference_mask:mask,target_snapshot_id:other,strength:.5,ev:0,preserve_persons:true,prompt:null});
 expect(api.selectionSnapshot.mock.calls[0][0].revision).toBe('9007199254740993');
});
it('retries an uncertain snapshot with its original key before creating intent',async()=>{
 const api=fixture();api.selectionSnapshot.mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce(snapshot);
 render(<SemanticCreate api={api} tenant={id} asset={id} version={id} mask={mask} collection={collection} onSubmitted={vi.fn()}/>);
 fireEvent.click(screen.getByText('Prepare batch review'));
 await waitFor(()=>expect(screen.getByText('Retry batch request')).toBeTruthy());
 expect(api.proposeSemanticIntent).not.toHaveBeenCalled();
 const retryButton=screen.getByRole('button',{name:'Retry batch request'});
 await waitFor(()=>expect(retryButton.hasAttribute('disabled')).toBe(false));
 fireEvent.click(retryButton);
 await waitFor(()=>expect(api.proposeSemanticIntent).toHaveBeenCalledTimes(1));
 expect(api.selectionSnapshot.mock.calls[0].slice(0,2)).toEqual(api.selectionSnapshot.mock.calls[1].slice(0,2));
});
it('starts a fresh journal after a confirmed expired semantic snapshot',async()=>{
 const api=fixture(),onSubmitted=vi.fn(),store={write:vi.fn().mockResolvedValue(undefined),clear:vi.fn().mockResolvedValue(undefined)} as unknown as SemanticAttemptStore;
 api.proposeSemanticIntent.mockRejectedValueOnce(new APIError(410,'SNAPSHOT_EXPIRED')).mockResolvedValueOnce(ticket);
 render(<SemanticCreate api={api} store={store} tenant={id} asset={id} version={id} mask={mask} collection={collection} onSubmitted={onSubmitted}/>);
 fireEvent.click(screen.getByText('Prepare batch review'));
 await waitFor(()=>expect(store.clear).toHaveBeenCalledTimes(1));
 const retryButton=screen.getByRole('button',{name:'Prepare batch review'});
 await waitFor(()=>expect(retryButton.hasAttribute('disabled')).toBe(false));
 fireEvent.click(retryButton);
 await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(ticket));
 expect(api.selectionSnapshot).toHaveBeenCalledTimes(2);
 expect(api.selectionSnapshot.mock.calls[0][1]).not.toBe(api.selectionSnapshot.mock.calls[1][1]);
 expect(api.proposeSemanticIntent.mock.calls[0][1]).not.toBe(api.proposeSemanticIntent.mock.calls[1][1]);
});
it('blocks a new semantic attempt when the expired journal cannot be cleared',async()=>{
 const api=fixture(),store={write:vi.fn().mockResolvedValue(undefined),clear:vi.fn().mockRejectedValue(new Error('storage unavailable'))} as unknown as SemanticAttemptStore;
 api.proposeSemanticIntent.mockRejectedValueOnce(new APIError(410,'SNAPSHOT_EXPIRED'));
 render(<SemanticCreate api={api} store={store} tenant={id} asset={id} version={id} mask={mask} collection={collection} onSubmitted={vi.fn()}/>);
 fireEvent.click(screen.getByText('Prepare batch review'));
 await waitFor(()=>expect(store.clear).toHaveBeenCalledTimes(1));
 await waitFor(()=>expect(screen.getByRole('button',{name:'Prepare batch review'}).hasAttribute('disabled')).toBe(true));
 expect(api.selectionSnapshot).toHaveBeenCalledTimes(1);
});
it('does not treat an unrelated 410 as an expired semantic snapshot',async()=>{
 const api=fixture(),store={write:vi.fn().mockResolvedValue(undefined),clear:vi.fn().mockResolvedValue(undefined)} as unknown as SemanticAttemptStore;
 api.proposeSemanticIntent.mockRejectedValueOnce(new APIError(410,'RESOURCE_REVOKED'));
 render(<SemanticCreate api={api} store={store} tenant={id} asset={id} version={id} mask={mask} collection={collection} onSubmitted={vi.fn()}/>);
 fireEvent.click(screen.getByText('Prepare batch review'));
 await screen.findByText(en.semantic_create_retry);expect(store.clear).not.toHaveBeenCalled();
 expect(screen.getByRole('button',{name:en.semantic_create_retry}).hasAttribute('disabled')).toBe(false);
 expect(api.proposeSemanticIntent).toHaveBeenCalledTimes(1);
});
it('refuses a changed asset version before freezing target collection',async()=>{
 const api=fixture();api.v2Binding.mockResolvedValue({...binding,asset_version_id:other});
 render(<SemanticCreate api={api} tenant={id} asset={id} version={id} mask={mask} collection={collection} onSubmitted={vi.fn()}/>);
 fireEvent.click(screen.getByText('Prepare batch review'));
 await waitFor(()=>expect(screen.getByRole('alert')).toBeTruthy());
 expect(api.selectionSnapshot).not.toHaveBeenCalled();expect(api.proposeSemanticIntent).not.toHaveBeenCalled();
 expect(screen.queryByText('Retry batch request')).toBeNull();
});
it('aborts stale source lookup when editor scope changes without sending it to the new collection',async()=>{
 const api=fixture(),onSubmitted=vi.fn();let resolve:(v:typeof binding)=>void=()=>{};
 api.v2Binding.mockImplementationOnce(()=>new Promise(r=>{resolve=r}));
 const view=render(<SemanticCreate api={api} tenant={id} asset={id} version={id} mask={mask} collection={collection} onSubmitted={onSubmitted}/>);
 fireEvent.click(screen.getByText('Prepare batch review'));
 await waitFor(()=>expect(api.v2Binding).toHaveBeenCalledTimes(1));
 const signal=api.v2Binding.mock.calls[0][1] as AbortSignal;
 view.rerender(<SemanticCreate api={api} tenant={id} asset={id} version={id} mask={mask} collection={{...collection,id:other}} onSubmitted={onSubmitted}/>);
 expect(signal.aborted).toBe(true);resolve(binding);
 await new Promise(r=>setTimeout(r,0));
 expect(api.selectionSnapshot).not.toHaveBeenCalled();expect(api.proposeSemanticIntent).not.toHaveBeenCalled();expect(onSubmitted).not.toHaveBeenCalled();
});
it.each(['ru','bg'] as const)('keeps uncertain request intact while switching locale to %s',async language=>{
 const api=fixture(),onSubmitted=vi.fn();api.proposeSemanticIntent.mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce(ticket);
 const view=render(<SemanticCreate api={api} tenant={id} asset={id} version={id} mask={mask} collection={collection} onSubmitted={onSubmitted}/>);
 fireEvent.click(screen.getByText(en.semantic_create_submit));
 await waitFor(()=>expect(screen.getByText(en.semantic_create_retry)).toBeTruthy());
 const original=structuredClone(api.proposeSemanticIntent.mock.calls[0].slice(0,2));
 locale.value=language;
 view.rerender(<SemanticCreate api={api} tenant={id} asset={id} version={id} mask={mask} collection={collection} onSubmitted={onSubmitted}/>);
 expect(screen.getByRole('alert').textContent).toBe(catalogs[language].semantic_create_unknown);
 const retryButton=screen.getByRole('button',{name:catalogs[language].semantic_create_retry});
 await waitFor(()=>expect(retryButton.hasAttribute('disabled')).toBe(false));
 fireEvent.click(retryButton);
 await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(ticket));
 expect(api.proposeSemanticIntent.mock.calls[1].slice(0,2)).toEqual(original);
 expect(api.selectionSnapshot).toHaveBeenCalledTimes(1);
});

it('stops after journal IO when availability is lost and preserves the original attempt for retry',async()=>{
 const api=fixture(),onSubmitted=vi.fn();
 let release!:()=>void;
 const pending=new Promise<void>(resolve=>{release=resolve});
 const write=vi.fn().mockImplementationOnce(()=>pending).mockResolvedValue(undefined);
 const store={write,clear:vi.fn().mockResolvedValue(undefined)} as unknown as SemanticAttemptStore;
 const props={api,tenant:id,asset:id,version:id,mask,collection,onSubmitted,store};
 const view=render(<SemanticCreate {...props}/>);
 fireEvent.click(screen.getByText('Prepare batch review'));
 await waitFor(()=>expect(write).toHaveBeenCalledTimes(1));
 const saved=structuredClone(write.mock.calls[0][0]);
 view.rerender(<SemanticCreate {...props} disabled/>);
 release();
 await screen.findByText('Retry batch request');
 expect(api.v2Binding).not.toHaveBeenCalled();
 expect(api.selectionSnapshot).not.toHaveBeenCalled();
 expect(api.proposeSemanticIntent).not.toHaveBeenCalled();
 view.rerender(<SemanticCreate {...props} disabled={false}/>);
 const retry=screen.getByRole('button',{name:'Retry batch request'});
 await waitFor(()=>expect(retry.hasAttribute('disabled')).toBe(false));
 fireEvent.click(retry);
 await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(ticket));
 expect(api.selectionSnapshot.mock.calls[0][1]).toBe(saved.snapshotKey);
 expect(api.proposeSemanticIntent.mock.calls[0][1]).toBe(saved.proposalKey);
});


it('persists a confirmed ticket when availability disappears during proposal and recovers without another POST',async()=>{
 const api=fixture();let acknowledge!:(value:typeof ticket)=>void;
 api.proposeSemanticIntent.mockImplementationOnce(()=>new Promise(resolve=>{acknowledge=resolve}));
 const write=vi.fn().mockResolvedValue(undefined);
 const store={write,clear:vi.fn()} as unknown as SemanticAttemptStore;
 const onSubmitted=vi.fn().mockImplementationOnce(()=>{throw new Error('navigation unavailable')});
 const props={api,tenant:id,asset:id,version:id,mask,collection,store,onSubmitted};
 const view=render(<SemanticCreate {...props}/>);
 fireEvent.click(screen.getByText('Prepare batch review'));
 await waitFor(()=>expect(api.proposeSemanticIntent).toHaveBeenCalledTimes(1));
 view.rerender(<SemanticCreate {...props} disabled/>);
 acknowledge(ticket);
 await screen.findByText('Retry batch request');
 expect(write.mock.calls.at(-1)?.[0].ticket).toEqual(ticket);
 const retry=screen.getByRole('button',{name:'Retry batch request'});
 await waitFor(()=>expect(retry.hasAttribute('disabled')).toBe(false));
 fireEvent.click(retry);
 await waitFor(()=>expect(onSubmitted).toHaveBeenCalledTimes(2));
 expect(api.proposeSemanticIntent).toHaveBeenCalledTimes(1);
 expect(api.selectionSnapshot).toHaveBeenCalledTimes(1);
 expect(write.mock.calls.at(-1)?.[0].ticket).toEqual(ticket);
});


it('blocks unavailable kinds and submits only the selected deployed kind',async()=>{
 const api=fixture(),onSubmitted=vi.fn();
 render(<SemanticCreate api={api} tenant={id} asset={id} version={id} mask={mask} collection={collection}
  kindAvailable={kind=>kind==='background_cleanup'} onSubmitted={onSubmitted}/>);
 expect(screen.getByRole('button',{name:en.semantic_create_submit}).hasAttribute('disabled')).toBe(true);
 expect(screen.getByRole('button',{name:en.semantic_create_remove}).hasAttribute('disabled')).toBe(true);
 expect(api.v2Binding).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole('button',{name:en.semantic_create_background}));
 fireEvent.click(screen.getByRole('button',{name:en.semantic_create_submit}));
 await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(ticket));
 expect(api.proposeSemanticIntent.mock.calls[0][0].kind).toBe('background_cleanup');
});


it('rechecks the original kind after async journal IO and retries with unchanged keys when deployment recovers',async()=>{
 const api=fixture(),onSubmitted=vi.fn();let release!:()=>void;
 const pending=new Promise<void>(resolve=>{release=resolve});
 const write=vi.fn().mockImplementationOnce(()=>pending).mockResolvedValue(undefined);
 const store={write,clear:vi.fn()} as unknown as SemanticAttemptStore;
 const props={api,tenant:id,asset:id,version:id,mask,collection,onSubmitted,store};
 const view=render(<SemanticCreate {...props} kindAvailable={()=>true}/>);
 fireEvent.click(screen.getByRole('button',{name:en.semantic_create_submit}));
 await waitFor(()=>expect(write).toHaveBeenCalledTimes(1));
 const frozen=structuredClone(write.mock.calls[0][0]);
 view.rerender(<SemanticCreate {...props} kindAvailable={kind=>kind==='background_cleanup'}/>);
 release();await screen.findByText(en.semantic_create_retry);
 expect(api.v2Binding).not.toHaveBeenCalled();
 expect(api.selectionSnapshot).not.toHaveBeenCalled();
 expect(api.proposeSemanticIntent).not.toHaveBeenCalled();
 expect(screen.getByRole('button',{name:en.semantic_create_retry}).hasAttribute('disabled')).toBe(true);
 view.rerender(<SemanticCreate {...props} kindAvailable={()=>true}/>);
 const retry=screen.getByRole('button',{name:en.semantic_create_retry});
 await waitFor(()=>expect(retry.hasAttribute('disabled')).toBe(false));fireEvent.click(retry);
 await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(ticket));
 expect(api.selectionSnapshot.mock.calls[0][1]).toBe(frozen.snapshotKey);
 expect(api.proposeSemanticIntent.mock.calls[0][1]).toBe(frozen.proposalKey);
 expect(api.proposeSemanticIntent.mock.calls[0][0].kind).toBe(frozen.kind);
});

it('replays a durably frozen unknown proposal with identical body/key after capability withdrawal and remount',async()=>{
 const api=fixture(),onSubmitted=vi.fn(),write=vi.fn().mockResolvedValue(undefined),store={write,clear:vi.fn()} as unknown as SemanticAttemptStore;
 api.proposeSemanticIntent.mockRejectedValueOnce(new Error('lost response')).mockResolvedValueOnce(ticket);
 const props={api,tenant:id,asset:id,version:id,mask,collection,onSubmitted,store};
 const first=render(<SemanticCreate {...props} kindAvailable={()=>true}/>);fireEvent.click(screen.getByRole('button',{name:en.semantic_create_submit}));
 await screen.findByText(en.semantic_create_retry);expect(api.proposeSemanticIntent).toHaveBeenCalledTimes(1);
 const original=api.proposeSemanticIntent.mock.calls[0],record=write.mock.calls.at(-1)?.[0];expect(record.body).toBeTruthy();expect(record.ticket).toBeNull();first.unmount();
 const restored={...record,collection:{...collection,...record.collection},binding:record.binding??undefined,snapshot:record.snapshot??undefined,body:record.body??undefined,ticket:record.ticket??undefined};delete restored.schema_version;
 render(<SemanticCreate {...props} initialAttempt={restored} kindAvailable={()=>false}/>);
 const submit=screen.getByRole('button',{name:en.semantic_create_submit});expect(submit.hasAttribute('disabled')).toBe(false);fireEvent.click(submit);
 await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(ticket));expect(api.proposeSemanticIntent.mock.calls[1].slice(0,2)).toEqual(original.slice(0,2));expect(api.selectionSnapshot).toHaveBeenCalledTimes(1);expect(api.v2Binding).toHaveBeenCalledTimes(1);
});

it('keeps structural edit denial for a frozen proposal even when capability recovery would be allowed',()=>{
 const api=fixture(),body={schema_version:1 as const,kind:'local_exposure' as const,reference:binding,reference_mask:mask,target_snapshot_id:other,strength:0.5,ev:0,preserve_persons:true as const,prompt:null};
 const initialAttempt={snapshotKey:id,proposalKey:other,collection,mask,kind:'local_exposure' as const,strength:0.5,ev:0,prompt:null,binding,snapshot:other,body};
 render(<SemanticCreate api={api} tenant={id} asset={id} version={id} mask={mask} collection={collection} disabled initialAttempt={initialAttempt} store={{write:vi.fn()} as unknown as SemanticAttemptStore} kindAvailable={()=>false} onSubmitted={vi.fn()}/>);
 expect(screen.getByRole('button',{name:en.semantic_create_submit}).hasAttribute('disabled')).toBe(true);expect(api.proposeSemanticIntent).not.toHaveBeenCalled();
});

it.each([[401,'UNAUTHORIZED'],[403,'FORBIDDEN'],[409,'IDEMPOTENCY_CONFLICT'],[410,'SNAPSHOT_EXPIRED'],[422,'VALIDATION_FAILED']] as const)('retains a previously uncertain frozen proposal after retry %s/%s',async(status,code)=>{
 const api=fixture(),onSubmitted=vi.fn(),write=vi.fn().mockResolvedValue(undefined),clear=vi.fn(),store={write,clear} as unknown as SemanticAttemptStore;
 api.proposeSemanticIntent.mockRejectedValueOnce(new Error('lost response')).mockRejectedValueOnce(new APIError(status,code)).mockResolvedValueOnce(ticket);
 render(<SemanticCreate api={api} tenant={id} asset={id} version={id} mask={mask} collection={collection} store={store} onSubmitted={onSubmitted}/>);
 await clickWhenEnabled(screen.getByRole('button',{name:en.semantic_create_submit}));await screen.findByText(en.semantic_create_retry);
 await clickWhenEnabled(screen.getByRole('button',{name:en.semantic_create_retry}));await waitFor(()=>expect(api.proposeSemanticIntent).toHaveBeenCalledTimes(2));
 const retry=screen.getByRole('button',{name:en.semantic_create_retry});await waitFor(()=>expect(retry.hasAttribute('disabled')).toBe(false));await clickWhenEnabled(retry);
 await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(ticket));expect(clear).not.toHaveBeenCalled();
 expect(api.proposeSemanticIntent.mock.calls[1].slice(0,2)).toEqual(api.proposeSemanticIntent.mock.calls[0].slice(0,2));expect(api.proposeSemanticIntent.mock.calls[2].slice(0,2)).toEqual(api.proposeSemanticIntent.mock.calls[0].slice(0,2));expect(api.selectionSnapshot).toHaveBeenCalledTimes(1);
});
it('Stop during durable body write preserves completed exact body for recovery after worker withdrawal',async()=>{
 const api=fixture(),onSubmitted=vi.fn();let finish!:()=>void;let paused=false;const write=vi.fn(async(value)=>{if(value.body&&!paused){paused=true;await new Promise<void>(done=>{finish=done});}}),store={write,clear:vi.fn()} as unknown as SemanticAttemptStore;
 const props={api,tenant:id,asset:id,version:id,mask,collection,onSubmitted,store};const view=render(<SemanticCreate {...props} kindAvailable={()=>true}/>);fireEvent.click(screen.getByText(en.semantic_create_submit));await waitFor(()=>expect(paused).toBe(true));fireEvent.click(screen.getByText(en.read_cancel));view.rerender(<SemanticCreate {...props} kindAvailable={()=>false}/>);await import('@testing-library/react').then(({act})=>act(async()=>finish()));fireEvent.click(screen.getByText(en.semantic_create_retry));await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(ticket));expect(api.proposeSemanticIntent).toHaveBeenCalledTimes(1);expect(api.selectionSnapshot).toHaveBeenCalledTimes(1);
});
it('bounds stalled storage wait and preserves accepted in-memory receipt for retry without POST',async()=>{
 vi.useFakeTimers();try{const api=fixture(),onSubmitted=vi.fn();let release!:()=>void,blocked=false;const store={write:vi.fn(async(value)=>{if(value.ticket&&!blocked){blocked=true;await new Promise<void>(done=>{release=done})}}),clear:vi.fn()} as unknown as SemanticAttemptStore;render(<SemanticCreate api={api} tenant={id} asset={id} version={id} mask={mask} collection={collection} store={store} onSubmitted={onSubmitted}/>);fireEvent.click(screen.getByText(en.semantic_create_submit));const {act}=await import('@testing-library/react');await act(async()=>{for(let i=0;i<12;i++)await Promise.resolve();await vi.advanceTimersByTimeAsync(15000)});expect(blocked).toBe(true);expect(screen.getByText(en.semantic_create_retry)).toBeTruthy();await act(async()=>release());fireEvent.click(screen.getByText(en.semantic_create_retry));await act(async()=>{for(let i=0;i<12;i++)await Promise.resolve()});expect(onSubmitted).toHaveBeenCalledWith(ticket);expect(api.proposeSemanticIntent).toHaveBeenCalledTimes(1);}finally{vi.useRealTimers();}
});
it('retained Stop for an old submission cannot abort a replacement exact replay',async()=>{
 const api=fixture(),onSubmitted=vi.fn();let first!:(value:typeof ticket)=>void,second!:(value:typeof ticket)=>void;api.proposeSemanticIntent.mockReturnValueOnce(new Promise(done=>{first=done})).mockReturnValueOnce(new Promise(done=>{second=done}));const store={write:vi.fn().mockResolvedValue(undefined)} as unknown as SemanticAttemptStore;render(<SemanticCreate api={api} tenant={id} asset={id} version={id} mask={mask} collection={collection} store={store} onSubmitted={onSubmitted}/>);fireEvent.click(screen.getByText(en.semantic_create_submit));await waitFor(()=>expect(api.proposeSemanticIntent).toHaveBeenCalledTimes(1));const oldStop=renderedStops.at(-1)!;fireEvent.click(screen.getByText(en.read_cancel));fireEvent.click(screen.getByText(en.semantic_create_retry));await waitFor(()=>expect(api.proposeSemanticIntent).toHaveBeenCalledTimes(2));const signal=api.proposeSemanticIntent.mock.calls[1][2] as AbortSignal;const {act}=await import('@testing-library/react');await act(async()=>{oldStop();first(ticket)});expect(signal.aborted).toBe(false);expect(onSubmitted).not.toHaveBeenCalled();await act(async()=>second(ticket));await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(ticket));
});

vi.mock('react-native-paper',async importOriginal=>{const paper=await importOriginal<typeof import('react-native-paper')>(),React=await import('react');const observe=(props:any)=>{const unwrap=(value:any):any=>typeof value==='string'?value:value?.props?unwrap(value.props.children):value;const label=unwrap(props.children);if(label==='Stop loading')renderedStops.push(props.onPress);};return {...paper,Button:(props:any)=>{observe(props);return React.createElement(paper.Button,props)},TouchableRipple:(props:any)=>{observe(props);return React.createElement(paper.TouchableRipple,props)}};});
