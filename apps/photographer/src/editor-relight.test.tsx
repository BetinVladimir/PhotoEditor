// @vitest-environment jsdom
import React from 'react';
import {clickWhenEnabled} from './test/ui-readiness';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
vi.mock('react-native',async()=>await import('react-native-web'));
const journal=vi.hoisted(()=>({saved:null as unknown,choices:{} as Record<string,unknown[]>}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{}}));
vi.mock('./relight-attempt-store',()=>({RelightAttemptStore:class{read=async()=>journal.saved;write=async()=>{};}}));
vi.mock('./relight-candidates',()=>({collectRelightCandidates:async(_fetch:any,query:any)=>journal.choices[query.kind]??[]}));
vi.mock('./relight-analysis-attempt-store',()=>({RelightAnalysisAttemptStore:class{}}));
vi.mock('./relight-analysis-action',()=>({RelightAnalysisAction:(props:any)=><button aria-label={props.intent.operation} disabled={props.disabled||props.capabilityAvailable===false}>Analysis</button>}));
vi.mock('./relight-controls',()=>({RelightControls:(props:any)=><div data-testid="relight-controls">{props.prepared.snapshot_id}</div>}));
import {EditorRelight} from './editor-relight';
import type {OwnerAPI} from './owner-api';
const id='11111111-1111-4111-8111-111111111111',other='22222222-2222-4222-8222-222222222222';
afterEach(()=>{cleanup();journal.saved=null;journal.choices={};});
it('restores unknown saved body before discovering new source or snapshot',async()=>{
 journal.saved={body:{snapshot_id:other}};
 const api={me:vi.fn().mockResolvedValue({id}),v2Binding:vi.fn(),createRenderInputSnapshot:vi.fn()};
 render(<EditorRelight api={api as unknown as OwnerAPI} origin="https://relight.test" tenant={id} project={id} asset={id} version={id} onSubmitted={vi.fn()}/>);
 expect((await screen.findByTestId('relight-controls')).textContent).toBe(other);
 expect(api.v2Binding).not.toHaveBeenCalled();expect(api.createRenderInputSnapshot).not.toHaveBeenCalled();
});
it('does not construct a new snapshot for a changed source version',async()=>{
 const api={me:vi.fn().mockResolvedValue({id}),v2Binding:vi.fn().mockResolvedValue({asset_version_id:other}),createRenderInputSnapshot:vi.fn()};
 render(<EditorRelight api={api as unknown as OwnerAPI} origin="https://relight.test" tenant={id} project={id} asset={id} version={id} onSubmitted={vi.fn()}/>);
 await waitFor(()=>expect(screen.getByRole('alert')).toBeTruthy());expect(api.createRenderInputSnapshot).not.toHaveBeenCalled();
});

it('gates depth and person analysis independently and updates when a worker disappears',async()=>{
 const api={me:vi.fn().mockResolvedValue({id}),v2Binding:vi.fn().mockResolvedValue({asset_version_id:id}),createRenderInputSnapshot:vi.fn().mockResolvedValue({id:other})};
 const props={api:api as unknown as OwnerAPI,origin:'https://relight.test',tenant:id,project:id,asset:id,version:id,onSubmitted:vi.fn()};
 const view=render(<EditorRelight {...props}/>);
 expect((await screen.findByLabelText('depth.predict') as HTMLButtonElement).disabled).toBe(true);
 expect((screen.getByLabelText('mask.person') as HTMLButtonElement).disabled).toBe(true);
 view.rerender(<EditorRelight {...props} depthAvailable personMaskAvailable/>);
 expect((screen.getByLabelText('depth.predict') as HTMLButtonElement).disabled).toBe(false);
 expect((screen.getByLabelText('mask.person') as HTMLButtonElement).disabled).toBe(false);
 view.rerender(<EditorRelight {...props} personMaskAvailable/>);
 expect((screen.getByLabelText('depth.predict') as HTMLButtonElement).disabled).toBe(true);
 expect((screen.getByLabelText('mask.person') as HTMLButtonElement).disabled).toBe(false);
});

it('permits independently available depth analysis while relight worker is absent',async()=>{
 const api={me:vi.fn().mockResolvedValue({id}),v2Binding:vi.fn().mockResolvedValue({asset_version_id:id}),createRenderInputSnapshot:vi.fn().mockResolvedValue({id:other})};
 render(<EditorRelight api={api as unknown as OwnerAPI} origin="https://relight.test" tenant={id} project={id} asset={id} version={id} capabilityAvailable={false} depthAvailable onSubmitted={vi.fn()}/>);
 expect((await screen.findByLabelText('depth.predict') as HTMLButtonElement).disabled).toBe(false);expect((screen.getByLabelText('mask.person') as HTMLButtonElement).disabled).toBe(true);
});

function binding(version=id){return {asset_version_id:version,source:{artifact_id:id,sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)}}
function relightProps(api:unknown){return {api:api as OwnerAPI,origin:'https://relight.test',tenant:id,project:id,asset:id,version:id,onSubmitted:vi.fn()}}
it('does not create a snapshot after hard gate changes during binding read',async()=>{
 let finish!:(value:unknown)=>void;const api={me:vi.fn().mockResolvedValue({id}),v2Binding:vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValue(binding()),createRenderInputSnapshot:vi.fn().mockResolvedValue({id:other})};
 const props=relightProps(api),view=render(<EditorRelight {...props}/>);await waitFor(()=>expect(api.v2Binding).toHaveBeenCalledOnce());
 const signal=api.v2Binding.mock.calls[0][1] as AbortSignal;view.rerender(<EditorRelight {...props} disabled/>);expect(signal.aborted).toBe(true);
 await act(async()=>finish(binding()));expect(api.createRenderInputSnapshot).not.toHaveBeenCalled();
 view.rerender(<EditorRelight {...props}/>);await screen.findByLabelText('depth.predict');expect(api.createRenderInputSnapshot).toHaveBeenCalledOnce();
});
it('retries interrupted snapshot creation with the same original body and key',async()=>{
 let finish!:(value:unknown)=>void;const api={me:vi.fn().mockResolvedValue({id}),v2Binding:vi.fn().mockResolvedValue(binding()),createRenderInputSnapshot:vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValue({id:other})};
 const props=relightProps(api),view=render(<EditorRelight {...props}/>);await waitFor(()=>expect(api.createRenderInputSnapshot).toHaveBeenCalledOnce());
 const original=api.createRenderInputSnapshot.mock.calls[0],signal=original[3] as AbortSignal;
 view.rerender(<EditorRelight {...props} disabled/>);expect(signal.aborted).toBe(true);view.rerender(<EditorRelight {...props}/>);
 await screen.findByLabelText('depth.predict');expect(api.createRenderInputSnapshot).toHaveBeenCalledTimes(2);expect(api.createRenderInputSnapshot.mock.calls[1].slice(0,3)).toEqual(original.slice(0,3));
 await act(async()=>finish({id:'33333333-3333-4333-8333-333333333333'}));expect(api.createRenderInputSnapshot).toHaveBeenCalledTimes(2);
});
it('does not reuse an interrupted snapshot key for changed source evidence',async()=>{
 let finish!:(value:unknown)=>void;const changed={...binding(),canvas_hash:'c'.repeat(64)};
 const api={me:vi.fn().mockResolvedValue({id}),v2Binding:vi.fn().mockResolvedValueOnce(binding()).mockResolvedValue(changed),createRenderInputSnapshot:vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve}))};
 const props=relightProps(api),view=render(<EditorRelight {...props}/>);await waitFor(()=>expect(api.createRenderInputSnapshot).toHaveBeenCalledOnce());
 view.rerender(<EditorRelight {...props} disabled/>);view.rerender(<EditorRelight {...props}/>);await screen.findByRole('alert');expect(api.createRenderInputSnapshot).toHaveBeenCalledOnce();await act(async()=>finish({id:other}));
});
it('ignores binding response from the previous photograph',async()=>{
 let finish!:(value:unknown)=>void;const api={me:vi.fn().mockResolvedValue({id}),v2Binding:vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValue(binding(other)),createRenderInputSnapshot:vi.fn().mockResolvedValue({id:other})};
 const props=relightProps(api),view=render(<EditorRelight {...props}/>);await waitFor(()=>expect(api.v2Binding).toHaveBeenCalledOnce());const signal=api.v2Binding.mock.calls[0][1] as AbortSignal;
 view.rerender(<EditorRelight {...props} asset={other} version={other}/>);await screen.findByLabelText('depth.predict');expect(signal.aborted).toBe(true);
 await act(async()=>finish(binding()));expect(api.createRenderInputSnapshot).toHaveBeenCalledOnce();expect(api.createRenderInputSnapshot.mock.calls[0][1]).toEqual({sources:[binding(other)]});
});
it('does not let a cancelled prepare response replace or unlock a new prepare',async()=>{
 const ref={artifact_id:id,sha256:'a'.repeat(64)};journal.choices={depth:[{kind:'depth',ref}],manual_target:[{kind:'manual_target',ref}],manual_guard:[{kind:'manual_guard',ref}]};
 let oldFinish!:(value:unknown)=>void,newFinish!:(value:unknown)=>void;
 const api={me:vi.fn().mockResolvedValue({id}),v2Binding:vi.fn().mockResolvedValue(binding()),createRenderInputSnapshot:vi.fn().mockResolvedValue({id:other}),prepareRelight:vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{oldFinish=resolve})).mockImplementationOnce(()=>new Promise(resolve=>{newFinish=resolve}))};
 const props=relightProps(api),view=render(<EditorRelight {...props}/>);await screen.findByLabelText('depth.predict');await clickWhenEnabled(await screen.findByRole('button',{name:'Prepare lighting'}));await waitFor(()=>expect(api.prepareRelight).toHaveBeenCalledOnce());const signal=api.prepareRelight.mock.calls[0][2] as AbortSignal;
 view.rerender(<EditorRelight {...props} disabled/>);expect(signal.aborted).toBe(true);view.rerender(<EditorRelight {...props}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'Prepare lighting'}));await waitFor(()=>expect(api.prepareRelight).toHaveBeenCalledTimes(2));
 await act(async()=>oldFinish({snapshot_id:id}));expect(screen.queryByTestId('relight-controls')).toBeNull();expect((screen.getByRole('button',{name:'Prepare lighting'}) as HTMLButtonElement).disabled).toBe(true);
 await act(async()=>newFinish({snapshot_id:other}));expect((await screen.findByTestId('relight-controls')).textContent).toBe(other);
});
