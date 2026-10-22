// @vitest-environment jsdom
import {afterEach,beforeEach,it,expect,vi} from 'vitest';
import {render,screen,fireEvent,waitFor,cleanup,act} from '@testing-library/react';
import type {OwnerAPI} from './owner-api';
import type {NegativeIntent} from './negative-intent';
import en from '../../../locales/en/common.json';
const state=vi.hoisted(()=>({values:new Map<string,string>(),listeners:new Set<(v:string)=>void>(),failWrite:false}));
vi.mock('react-native',async()=>({...await import('react-native-web'),AppState:{currentState:'active',addEventListener:(_name:string,fn:(v:string)=>void)=>{state.listeners.add(fn);return {remove:()=>state.listeners.delete(fn)}}}}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(k:string)=>state.values.get(k)??null,setItem:async(k:string,v:string)=>{if(state.failWrite)throw new Error('storage unavailable');state.values.set(k,v)},removeItem:async(k:string)=>{state.values.delete(k)},getAllKeys:async()=>[...state.values.keys()]}}));
vi.mock('./semantic-attempt-store',()=>({semanticJournalLock:async(_origin:string,action:()=>Promise<unknown>)=>action()}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:keyof typeof en)=>en[key]})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
import {EditorNegative} from './editor-negative';
import {clickWhenEnabled} from './test/ui-readiness';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ticket={run_id:id(10),workflow_id:id(11),state:'queued' as const};
function fixture(){
 const api={me:vi.fn().mockResolvedValue({id:id(1)}),negativeCapabilities:vi.fn().mockResolvedValue({schema_version:1,tenant_id:id(2),project_id:id(3),calibration:{artifact_id:id(4),sha256:'a'.repeat(64)},max_pixels:10000}),v2Binding:vi.fn().mockResolvedValue({asset_version_id:id(5),source:{artifact_id:id(6),sha256:'b'.repeat(64)},canvas_hash:'c'.repeat(64)}),createRenderInputSnapshot:vi.fn().mockResolvedValue({id:id(7)}),submitNegative:vi.fn().mockResolvedValue(ticket),get:vi.fn().mockResolvedValue(ticket)};
 const onSubmitted=vi.fn();
 const props={api:api as unknown as OwnerAPI,origin:'https://negative-dom.test',tenant:id(2),project:id(3),asset:id(8),version:id(5),onSubmitted};
 return {api,props,onSubmitted};
}
async function ready(){await waitFor(()=>expect(screen.getByLabelText(en.negative_strength)).toBeTruthy())}
beforeEach(()=>{state.values.clear();state.failWrite=false});afterEach(()=>{cleanup();state.listeners.clear()});
it('sends configured calibration and numeric settings only after persistence',async()=>{
 const {api,props,onSubmitted}=fixture();api.submitNegative.mockImplementation(async()=>{expect([...state.values.keys()].some(k=>k.startsWith('photo.negative-attempt.v1:'))).toBe(true);return ticket});
 render(<EditorNegative {...props}/>);await ready();
 fireEvent.change(screen.getByLabelText(en.negative_strength),{target:{value:'0.75'}});fireEvent.change(screen.getByLabelText(en.negative_exposure),{target:{value:'-1.5'}});await clickWhenEnabled(screen.getByRole('button',{name:en.negative_submit}));
 await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(ticket));
 expect(api.submitNegative).toHaveBeenCalledTimes(1);const request=api.submitNegative.mock.calls[0][1] as NegativeIntent;
 expect(request.params.strength).toBe(.75);expect(request.params.exposure_ev).toBe(-1.5);expect(request.params.calibration.artifact_id).toBe(id(4));
});
it('remount recovers original key/body after response loss without a new snapshot',async()=>{
 const {api,props,onSubmitted}=fixture();api.submitNegative.mockRejectedValueOnce(new Error('response lost')).mockResolvedValue(ticket);
 const first=render(<EditorNegative {...props}/>);await ready();await clickWhenEnabled(screen.getByRole('button',{name:en.negative_submit}));
 await waitFor(()=>expect(screen.getByText(en.negative_unknown)).toBeTruthy());expect((screen.getByLabelText(en.negative_strength) as HTMLInputElement).readOnly).toBe(true);
 const original=structuredClone(api.submitNegative.mock.calls[0].slice(0,3));first.unmount();
 render(<EditorNegative {...props}/>);await ready();await clickWhenEnabled(screen.getByRole('button',{name:en.negative_retry}));await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(ticket));
 expect(api.submitNegative.mock.calls[1].slice(0,3)).toEqual(original);expect(api.createRenderInputSnapshot).toHaveBeenCalledTimes(1);expect(api.negativeCapabilities).toHaveBeenCalledTimes(1);
});
it('rechecks workflow authority on a cached acknowledgement without resending',async()=>{
 const {api,props,onSubmitted}=fixture();api.get.mockResolvedValueOnce({...ticket,run_id:id(99)}).mockResolvedValue(ticket);
 render(<EditorNegative {...props}/>);await ready();await clickWhenEnabled(screen.getByRole('button',{name:en.negative_submit}));await waitFor(()=>expect(screen.getByText(en.negative_unknown)).toBeTruthy());expect(onSubmitted).not.toHaveBeenCalled();
 await clickWhenEnabled(screen.getByRole('button',{name:en.negative_open}));await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(ticket));expect(api.submitNegative).toHaveBeenCalledTimes(1);expect(api.get).toHaveBeenCalledTimes(2);
});
it('starts another negative with fresh snapshot and calibration after an accepted result',async()=>{
 const {api,props,onSubmitted}=fixture();
 api.createRenderInputSnapshot.mockResolvedValueOnce({id:id(7)}).mockResolvedValueOnce({id:id(70)});
 api.negativeCapabilities.mockResolvedValueOnce({schema_version:1,tenant_id:id(2),project_id:id(3),calibration:{artifact_id:id(4),sha256:'a'.repeat(64)},max_pixels:10000})
  .mockResolvedValueOnce({schema_version:1,tenant_id:id(2),project_id:id(3),calibration:{artifact_id:id(40),sha256:'d'.repeat(64)},max_pixels:10000});
 render(<EditorNegative {...props}/>);await ready();
 fireEvent.change(screen.getByLabelText(en.negative_strength),{target:{value:'0.7'}});
 await clickWhenEnabled(screen.getByRole('button',{name:en.negative_submit}));await waitFor(()=>expect(onSubmitted).toHaveBeenCalledTimes(1));
 await clickWhenEnabled(screen.getByRole('button',{name:en.negative_another}));await waitFor(()=>expect(screen.getByText(en.negative_submit)).toBeTruthy());
 expect((screen.getByLabelText(en.negative_strength) as HTMLInputElement).value).toBe('0.7');
 await clickWhenEnabled(screen.getByRole('button',{name:en.negative_submit}));await waitFor(()=>expect(api.submitNegative).toHaveBeenCalledTimes(2));
 const first=api.submitNegative.mock.calls[0][1] as NegativeIntent,second=api.submitNegative.mock.calls[1][1] as NegativeIntent;
 expect(first.snapshot_id).toBe(id(7));expect(second.snapshot_id).toBe(id(70));
 expect(first.params.calibration.artifact_id).toBe(id(4));expect(second.params.calibration.artifact_id).toBe(id(40));
 expect(second.params.strength).toBe(.7);
 expect(api.submitNegative.mock.calls[1][2]).not.toBe(api.submitNegative.mock.calls[0][2]);
});
it('does not send when journal persistence fails',async()=>{
 const {api,props}=fixture();render(<EditorNegative {...props}/>);await ready();state.failWrite=true;await clickWhenEnabled(screen.getByRole('button',{name:en.negative_submit}));await waitFor(()=>expect(screen.getByText(en.negative_unknown)).toBeTruthy());expect(api.submitNegative).not.toHaveBeenCalled();
});
it('rejects invalid numeric values before creating an attempt',async()=>{
 const {api,props}=fixture();render(<EditorNegative {...props}/>);await ready();fireEvent.change(screen.getByLabelText(en.negative_red),{target:{value:'0'}});await clickWhenEnabled(screen.getByRole('button',{name:en.negative_submit}));await waitFor(()=>expect(screen.getByText(en.negative_invalid)).toBeTruthy());expect(api.submitNegative).not.toHaveBeenCalled();expect(screen.getByText(en.negative_submit)).toBeTruthy();
});
it('ignores a late response after entering background and restores the journal',async()=>{
 const {api,props,onSubmitted}=fixture();let resolve!:(v:typeof ticket)=>void;api.submitNegative.mockImplementation(()=>new Promise(r=>{resolve=r}));
 render(<EditorNegative {...props}/>);await ready();await clickWhenEnabled(screen.getByRole('button',{name:en.negative_submit}));await waitFor(()=>expect(api.submitNegative).toHaveBeenCalledTimes(1));
 act(()=>{for(const listener of state.listeners)listener('background')});await act(async()=>resolve(ticket));expect(onSubmitted).not.toHaveBeenCalled();
 act(()=>{for(const listener of state.listeners)listener('active')});await ready();expect(screen.getByText(en.negative_open)).toBeTruthy();expect(api.createRenderInputSnapshot).toHaveBeenCalledTimes(1);
});

it('blocks negative submission after live capability withdrawal without rerender',async()=>{
 const {api,props,onSubmitted}=fixture();let available=true;
 render(<EditorNegative {...props} operationAvailable={()=>available}/>);await ready();
 available=false;fireEvent.click(screen.getByText(en.negative_submit));
 expect(api.submitNegative).not.toHaveBeenCalled();expect(onSubmitted).not.toHaveBeenCalled();
 available=true;await clickWhenEnabled(screen.getByRole('button',{name:en.negative_submit}));
 await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(ticket));expect(api.submitNegative).toHaveBeenCalledTimes(1);
});

it('opens accepted negative result without worker availability or another POST',async()=>{
 const {api,props,onSubmitted}=fixture();let available=true;
 const view=render(<EditorNegative {...props} operationAvailable={()=>available}/>);await ready();
 await clickWhenEnabled(screen.getByRole('button',{name:en.negative_submit}));await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(ticket));
 onSubmitted.mockClear();available=false;view.unmount();
 render(<EditorNegative {...props} operationAvailable={()=>available}/>);await ready();
 await clickWhenEnabled(screen.getByRole('button',{name:en.negative_open}));
 await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(ticket));
 expect(api.submitNegative).toHaveBeenCalledTimes(1);expect(api.createRenderInputSnapshot).toHaveBeenCalledTimes(1);expect(api.get).toHaveBeenCalledTimes(2);
});
