// @vitest-environment jsdom
import React from 'react';
import {act,cleanup,fireEvent,render,screen} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
const fixture=vi.hoisted(()=>({presets:vi.fn(),listeners:new Set<(state:string)=>void>()}));
vi.mock('react-native',async()=>({...await import('react-native-web'),AppState:{currentState:'active',addEventListener:(_event:string,listener:(state:string)=>void)=>{fixture.listeners.add(listener);return {remove:()=>fixture.listeners.delete(listener)};}}}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({locale:'en'})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('./owner-api',()=>({OwnerAPI:class{presets=fixture.presets;}}));
import {SharePresetPicker} from './share-preset-picker';
const tenant='11111111-1111-4111-8111-111111111111',id='22222222-2222-4222-8222-222222222222';
async function flush(){await act(async()=>{for(let i=0;i<12;i++)await Promise.resolve();});}
afterEach(()=>{cleanup();vi.clearAllMocks();fixture.listeners.clear();});
it('resets paging when the API client changes',async()=>{
 fixture.presets.mockImplementation((cursor?:string)=>Promise.resolve({items:[{id,name:cursor?'Second format':'First format'}],next_cursor:cursor?null:'page-two'}));
 const first={} as any,second={} as any,onChange=vi.fn();
 const view=render(<SharePresetPicker client={first} tenant={tenant} value={[]} onChange={onChange} disabled={false}/>);await flush();
 fireEvent.click(screen.getByText('More formats'));await flush();expect(fixture.presets.mock.calls.at(-1)?.[0]).toBe('page-two');
 view.rerender(<SharePresetPicker client={second} tenant={tenant} value={[]} onChange={onChange} disabled={false}/>);await flush();
 expect(fixture.presets.mock.calls.at(-1)?.[0]).toBeUndefined();expect(screen.getByText('First format')).toBeTruthy();
});
it('does not publish a noncooperative old-tenant response',async()=>{
 let finish!:(page:unknown)=>void;
 fixture.presets.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve;})).mockResolvedValue({items:[{id,name:'Current format'}],next_cursor:null});
 const client={} as any,onChange=vi.fn(),view=render(<SharePresetPicker client={client} tenant={tenant} value={[]} onChange={onChange} disabled={false}/>);await flush();
 view.rerender(<SharePresetPicker client={client} tenant="33333333-3333-4333-8333-333333333333" value={[]} onChange={onChange} disabled={false}/>);await flush();
 finish({items:[{id,name:'Old private format'}],next_cursor:null});await flush();
 expect(screen.queryByText('Old private format')).toBeNull();expect(screen.getByText('Current format')).toBeTruthy();expect(onChange).not.toHaveBeenCalled();
});
it('refuses oversized cursor metadata and does not offer the malformed page',async()=>{
 fixture.presets.mockResolvedValue({items:[{id,name:'Malformed page'}],next_cursor:'x'.repeat(4097)});
 render(<SharePresetPicker client={{} as any} tenant={tenant} value={[]} onChange={vi.fn()} disabled={false}/>);await flush();
 expect(screen.queryByText('Malformed page')).toBeNull();expect(screen.getByText('Retry formats')).toBeTruthy();
});
