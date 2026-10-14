// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
import {PresetMetadataEditor} from './preset-metadata-editor';
import {APIError} from '@photoeditor/api-client';
import type {ExportPreset} from './catalog-types';
const preset={id:'00000000-0000-4000-8000-000000000001',name:'Print',revision:'1',config:{metadata_policy:'strip'}} as ExportPreset;
afterEach(cleanup);
it('retries an unknown result with the exact policy and idempotency key',async()=>{
 const change=vi.fn().mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce({...preset,revision:'2'}),saved=vi.fn();
 render(<PresetMetadataEditor api={{changePresetMetadataPolicy:change} as never} preset={preset} onSaved={saved} onCancel={()=>{}}/>);
 fireEvent.click(screen.getByRole('radio',{name:'Author and copyright only'}));
 fireEvent.click(screen.getByText('Save'));
 fireEvent.click(await screen.findByText('Retry save'));
 await waitFor(()=>expect(saved).toHaveBeenCalled());
 expect(change.mock.calls[0].slice(0,3)).toEqual(change.mock.calls[1].slice(0,3));
 expect(change.mock.calls[0][1]).toBe('copyright');
});
it('aborts unmounted saves and ignores late responses',async()=>{
 let finish!:(value:ExportPreset)=>void;
 const change=vi.fn().mockImplementation(()=>new Promise(resolve=>{finish=resolve})),saved=vi.fn();
 const view=render(<PresetMetadataEditor api={{changePresetMetadataPolicy:change} as never} preset={preset} onSaved={saved} onCancel={()=>{}}/>);
 fireEvent.click(screen.getByRole('radio',{name:'Author and copyright only'}));fireEvent.click(screen.getByText('Save'));
 await waitFor(()=>expect(change).toHaveBeenCalled());view.unmount();expect(change.mock.calls[0][3].aborted).toBe(true);
 await act(async()=>finish({...preset,revision:'2'}));expect(saved).not.toHaveBeenCalled();
});

it('stops retry after revision conflict and keeps the current preset unchanged',async()=>{
 const change=vi.fn().mockRejectedValue(new APIError(409,'REVISION_CONFLICT')),saved=vi.fn(),cancel=vi.fn();
 render(<PresetMetadataEditor api={{changePresetMetadataPolicy:change} as never} preset={preset} onSaved={saved} onCancel={cancel}/>);
 fireEvent.click(screen.getByRole('radio',{name:'Author and copyright only'}));fireEvent.click(screen.getByText('Save'));
 await screen.findByText('Preset changed or access was lost. Close and reload presets.');
 fireEvent.click(screen.getByText('Save'));expect(change).toHaveBeenCalledTimes(1);expect(saved).not.toHaveBeenCalled();
 fireEvent.click(screen.getByText('Cancel'));expect(cancel).toHaveBeenCalledTimes(1);
});
