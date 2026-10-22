// @vitest-environment jsdom
import {expectEnabled,clickWhenEnabled} from './test/ui-readiness';
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
import {PresetMetadataEditor} from './preset-metadata-editor';
import {APIError} from '@photoeditor/api-client';
import type {ExportPreset} from './catalog-types';
import {exportPresetConfigHash} from './export-preset-response';
function resource(revision='1',metadata_policy:'strip'|'copyright'='strip'):ExportPreset{const config={format:'png' as const,icc_id:'srgb_v1' as const,subsampling:'444' as const,alpha:'preserve' as const,metadata_policy,naming_template:'{name}'};return {id:'00000000-0000-4000-8000-000000000001',name:'Print',revision,config,config_hash:exportPresetConfigHash(config),created_at:'2026-10-17T00:00:00Z'}}
const preset=resource();
afterEach(cleanup);
it('retries an unknown result with the exact policy and idempotency key',async()=>{
 const change=vi.fn().mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce(resource('2','copyright')),saved=vi.fn();
 render(<PresetMetadataEditor api={{changePresetMetadataPolicy:change} as never} preset={preset} onSaved={saved} onCancel={()=>{}}/>);
 await clickWhenEnabled(screen.getByRole('radio',{name:'Author and copyright only'}));
 await clickWhenEnabled(screen.getByRole('button',{name:'Save'}));
 await clickWhenEnabled(await screen.findByRole('button',{name:'Retry save'}));
 await waitFor(()=>expect(saved).toHaveBeenCalled());
 expect(change.mock.calls[0].slice(0,3)).toEqual(change.mock.calls[1].slice(0,3));
 expect(change.mock.calls[0][1]).toBe('copyright');
});
it('aborts unmounted saves and ignores late responses',async()=>{
 let finish!:(value:ExportPreset)=>void;
 const change=vi.fn().mockImplementation(()=>new Promise(resolve=>{finish=resolve})),saved=vi.fn();
 const view=render(<PresetMetadataEditor api={{changePresetMetadataPolicy:change} as never} preset={preset} onSaved={saved} onCancel={()=>{}}/>);
 await clickWhenEnabled(screen.getByRole('radio',{name:'Author and copyright only'}));await clickWhenEnabled(screen.getByRole('button',{name:'Save'}));
 await waitFor(()=>expect(change).toHaveBeenCalled());view.unmount();expect(change.mock.calls[0][3].aborted).toBe(true);
 await act(async()=>finish({...preset,revision:'2'}));expect(saved).not.toHaveBeenCalled();
});

it('stops retry after revision conflict and keeps the current preset unchanged',async()=>{
 const change=vi.fn().mockRejectedValue(new APIError(409,'REVISION_CONFLICT')),saved=vi.fn(),cancel=vi.fn();
 render(<PresetMetadataEditor api={{changePresetMetadataPolicy:change} as never} preset={preset} onSaved={saved} onCancel={cancel}/>);
 await clickWhenEnabled(screen.getByRole('radio',{name:'Author and copyright only'}));await clickWhenEnabled(screen.getByRole('button',{name:'Save'}));
 await screen.findByText('Preset changed or access was lost. Close and reload presets.');
 fireEvent.click(screen.getByText('Save'));expect(change).toHaveBeenCalledTimes(1);expect(saved).not.toHaveBeenCalled();
 await clickWhenEnabled(screen.getByRole('button',{name:'Cancel'}));expect(cancel).toHaveBeenCalledTimes(1);
});

it('recovers a frozen metadata request after remount, including its original revision',async()=>{
 const record={version:1 as const,key:'00000000-0000-4000-8000-000000000008',preset:preset.id,revision:'1',mode:'copyright' as const};
 const journal={read:vi.fn().mockResolvedValue(record),write:vi.fn().mockResolvedValue(undefined),clearAccepted:vi.fn().mockResolvedValue(undefined)};
 const change=vi.fn().mockResolvedValue(resource('2','copyright')),saved=vi.fn();
 render(<PresetMetadataEditor journal={journal as never} api={{changePresetMetadataPolicy:change} as never} preset={resource('2','copyright')} onSaved={saved} onCancel={()=>{}}/>);
 const retry=await screen.findByRole('button',{name:'Retry save'});
 await waitFor(()=>{expect(journal.read).toHaveBeenCalledOnce();expect((retry as HTMLButtonElement).disabled).toBe(false)});
 await clickWhenEnabled(retry);await waitFor(()=>expect(saved).toHaveBeenCalled());
 expect(change.mock.calls[0][0].revision).toBe('1');expect(change.mock.calls[0][1]).toBe('copyright');expect(change.mock.calls[0][2]).toBe(record.key);
 expect(journal.write).toHaveBeenCalledWith(record);expect(journal.clearAccepted).toHaveBeenCalledWith(record,expect.objectContaining({revision:'2'}));
});
it('does not send a metadata PATCH when immutable journal persistence fails',async()=>{
 const journal={read:vi.fn().mockResolvedValue(null),write:vi.fn().mockRejectedValue(new Error('storage')),clearAccepted:vi.fn()},change=vi.fn();
 render(<PresetMetadataEditor journal={journal as never} api={{changePresetMetadataPolicy:change} as never} preset={preset} onSaved={()=>{}} onCancel={()=>{}}/>);
 await waitFor(()=>expectEnabled(screen.getByRole('radio',{name:'Author and copyright only'})));
 await clickWhenEnabled(screen.getByRole('radio',{name:'Author and copyright only'}));await clickWhenEnabled(screen.getByRole('button',{name:'Save'}));await screen.findByText('Retry save');expect(change).not.toHaveBeenCalled();
 await clickWhenEnabled(screen.getByRole('button',{name:'Retry save'}));await waitFor(()=>expect(journal.write).toHaveBeenCalledTimes(2));expect(journal.write.mock.calls[1][0]).toEqual(journal.write.mock.calls[0][0]);expect(change).not.toHaveBeenCalled();
});

it('retains the exact journal when a response changes another parameter despite a valid hash',async()=>{
 const record={version:1 as const,key:'00000000-0000-4000-8000-000000000008',preset:preset.id,revision:'1',mode:'copyright' as const,baseline:preset};
 const journal={read:vi.fn().mockResolvedValue(record),write:vi.fn().mockResolvedValue(undefined),clearAccepted:vi.fn()};
 const changed=resource('2','copyright');changed.config.naming_template='changed';changed.config_hash=exportPresetConfigHash(changed.config);
 const change=vi.fn().mockResolvedValue(changed),saved=vi.fn();render(<PresetMetadataEditor journal={journal as never} api={{changePresetMetadataPolicy:change} as never} preset={preset} onSaved={saved} onCancel={()=>{}}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'Retry save'}));await screen.findByText('Unable to save. Retry sends the same request.');
 expect(journal.clearAccepted).not.toHaveBeenCalled();expect(saved).not.toHaveBeenCalled();expect(change.mock.calls[0][2]).toBe(record.key);
});
it('recovers a legacy key after newer catalog parameters changed without treating them as original baseline',async()=>{
 const record={version:1 as const,key:'00000000-0000-4000-8000-000000000008',preset:preset.id,revision:'1',mode:'copyright' as const};
 const journal={read:vi.fn().mockResolvedValue(record),write:vi.fn().mockResolvedValue(undefined),clearAccepted:vi.fn().mockResolvedValue(undefined)};
 const current=resource('3','copyright');current.config.naming_template='newer';current.config_hash=exportPresetConfigHash(current.config);
 const change=vi.fn().mockResolvedValue(resource('2','copyright')),saved=vi.fn();render(<PresetMetadataEditor journal={journal as never} api={{changePresetMetadataPolicy:change} as never} preset={current} onSaved={saved} onCancel={()=>{}}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'Retry save'}));await waitFor(()=>expect(saved).toHaveBeenCalledOnce());expect(change.mock.calls[0][0].revision).toBe('1');expect(change.mock.calls[0][2]).toBe(record.key);expect(change.mock.calls[0][4]).toBeNull();
});

it('gives an immediate retry its own full deadline and ignores old settlement',async()=>{
 vi.useFakeTimers();let old!:(value:ExportPreset)=>void,newer!:(value:ExportPreset)=>void;
 const change=vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{old=resolve})).mockImplementationOnce(()=>new Promise(resolve=>{newer=resolve}));
 const saved=vi.fn();
 try{
  render(<PresetMetadataEditor api={{changePresetMetadataPolicy:change} as never} preset={preset} onSaved={saved} onCancel={()=>{}}/>);
  fireEvent.click(screen.getByRole('radio',{name:'Author and copyright only'}));fireEvent.click(screen.getByText('Save'));
  expect(change).toHaveBeenCalledTimes(1);const oldSignal=change.mock.calls[0][3] as AbortSignal;
  await act(async()=>{await vi.advanceTimersByTimeAsync(10000)});
  fireEvent.click(screen.getByText('Stop loading'));fireEvent.click(screen.getByText('Retry save'));
  expect(oldSignal.aborted).toBe(true);expect(change).toHaveBeenCalledTimes(2);const nextSignal=change.mock.calls[1][3] as AbortSignal;
  expect(change.mock.calls[1].slice(0,3)).toEqual(change.mock.calls[0].slice(0,3));
  await act(async()=>{await vi.advanceTimersByTimeAsync(5000);old(resource('2','copyright'))});
  expect(nextSignal.aborted).toBe(false);expect(screen.getByText('Stop loading')).toBeTruthy();expect(saved).not.toHaveBeenCalled();
  await act(async()=>{await vi.advanceTimersByTimeAsync(9999)});expect(nextSignal.aborted).toBe(false);
  await act(async()=>{await vi.advanceTimersByTimeAsync(1)});expect(nextSignal.aborted).toBe(true);
  await act(async()=>{newer(resource('2','copyright'))});expect(saved).not.toHaveBeenCalled();
 }finally{vi.useRealTimers()}
});
it('stops a save exactly at its owned fifteen-second boundary without adopting a late receipt',async()=>{
 vi.useFakeTimers();let finish!:(value:ExportPreset)=>void;const change=vi.fn().mockImplementation(()=>new Promise(resolve=>{finish=resolve})),saved=vi.fn();
 try{
  render(<PresetMetadataEditor api={{changePresetMetadataPolicy:change} as never} preset={preset} onSaved={saved} onCancel={()=>{}}/>);
  fireEvent.click(screen.getByRole('radio',{name:'Author and copyright only'}));fireEvent.click(screen.getByText('Save'));
  const signal=change.mock.calls[0][3] as AbortSignal;
  await act(async()=>{await vi.advanceTimersByTimeAsync(14999)});expect(signal.aborted).toBe(false);
  await act(async()=>{await vi.advanceTimersByTimeAsync(1)});expect(signal.aborted).toBe(true);expect(screen.getByText('Retry save')).toBeTruthy();
  await act(async()=>{finish(resource('2','copyright'))});expect(saved).not.toHaveBeenCalled();
 }finally{vi.useRealTimers()}
});
