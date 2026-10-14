// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen,waitFor,act} from '@testing-library/react';
import type {MultiIntent} from './multi-intent';
import type {OwnerAPI} from './owner-api';
import en from '../../../locales/en/common.json';
const mocks=vi.hoisted(()=>({read:vi.fn(),resume:vi.fn(),clear:vi.fn()}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{}}));
vi.mock('./locale-context',()=>({useLocale:()=>({t:(key:keyof typeof en)=>en[key]})}));
vi.mock('./multi-apply-store',()=>({MultiApplyStore:class{read=mocks.read;clear=mocks.clear;}}));
vi.mock('./multi-apply-attempt',()=>({MultiApplyAttempt:{restore:(record:unknown)=>({snapshot:record,resume:mocks.resume})}}));
import {MultiApplyRecovery} from './multi-apply-recovery';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const api={me:async()=>({id:id(1)})} as unknown as OwnerAPI;
const props={api,origin:'https://studio.test',tenant:id(2),project:id(3),producer:id(4),collection:id(5),operation:'optical.panorama' as const,intent:{schema_version:1,snapshot_id:id(7)} as MultiIntent};
const scoped={review:{proposal:{operation:'optical.panorama'}},intent:props.intent};
// UI isolation fixtures; journal schema validation is covered by attempt/store unit suites.
afterEach(()=>{cleanup();vi.resetAllMocks();});
it('resumes the persisted attempt and does not clear an incomplete collection CAS',async()=>{
 mocks.read.mockResolvedValue({...scoped,selection:{collection:id(5),revision:'3'},applied:{asset_version_id:id(6)},selected:false});
 mocks.resume.mockResolvedValue({asset_version_id:id(6)});const onApplied=vi.fn();
 render(<MultiApplyRecovery {...props} onApplied={onApplied}/>);
 fireEvent.click(await screen.findByText(en.perspective_apply_resume));
 await waitFor(()=>expect(onApplied).toHaveBeenCalledWith({asset_version_id:id(6)}));
 expect(screen.queryByText(en.perspective_apply_clear_completed)).toBeNull();expect(mocks.clear).not.toHaveBeenCalled();
});
it('refuses a persisted selection belonging to another collection',async()=>{
 mocks.read.mockResolvedValue({...scoped,selection:{collection:id(9)},applied:null,selected:false});
 render(<MultiApplyRecovery {...props} onApplied={vi.fn()}/>);
 await screen.findByText(en.perspective_apply_failed);
 expect(screen.queryByText(en.perspective_apply_resume)).toBeNull();expect(mocks.resume).not.toHaveBeenCalled();
});
it('refuses another frozen intent even when producer and collection match',async()=>{
 mocks.read.mockResolvedValue({...scoped,intent:{...props.intent,snapshot_id:id(80)},selection:{collection:id(5)},applied:null,selected:false});
 render(<MultiApplyRecovery {...props} onApplied={vi.fn()}/>);
 await screen.findByText(en.perspective_apply_failed);
 expect(screen.queryByText(en.perspective_apply_resume)).toBeNull();expect(mocks.resume).not.toHaveBeenCalled();
});
it('ignores late journal delivery from a replaced tenant scope',async()=>{
 let finish!:(record:unknown)=>void;
 mocks.read.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve;})).mockResolvedValueOnce(null);
 const mounted=render(<MultiApplyRecovery {...props} onApplied={vi.fn()}/>);
 await waitFor(()=>expect(mocks.read).toHaveBeenCalledTimes(1));
 mounted.rerender(<MultiApplyRecovery {...props} tenant={id(90)} onApplied={vi.fn()}/>);
 await waitFor(()=>expect(mocks.read).toHaveBeenCalledTimes(2));
 finish({...scoped,selection:{collection:id(5)},applied:null,selected:false});
 await waitFor(()=>expect(screen.queryByText(en.perspective_apply_resume)).toBeNull());
 expect(mocks.resume).not.toHaveBeenCalled();
});

it('resets busy for new scope and old completion cannot unlock its active resume',async()=>{
 const record={...scoped,selection:null,applied:null,selected:false};mocks.read.mockResolvedValue(record);
 let finishOld!:(value:unknown)=>void,finishNew!:(value:unknown)=>void;
 mocks.resume.mockImplementationOnce(()=>new Promise(resolve=>{finishOld=resolve;}))
  .mockImplementationOnce(()=>new Promise(resolve=>{finishNew=resolve;}));
 const oldApplied=vi.fn(),newApplied=vi.fn();
 const mounted=render(<MultiApplyRecovery {...props} onApplied={oldApplied}/>);
 fireEvent.click(await screen.findByText(en.perspective_apply_resume));
 await waitFor(()=>expect(mocks.resume).toHaveBeenCalledTimes(1));
 mounted.rerender(<MultiApplyRecovery {...props} tenant={id(90)} onApplied={newApplied}/>);
 await waitFor(()=>expect(mocks.read).toHaveBeenCalledTimes(2));
 await waitFor(()=>expect(screen.getByRole('button',{name:en.perspective_apply_resume}).hasAttribute('disabled')).toBe(false));
 fireEvent.click(screen.getByRole('button',{name:en.perspective_apply_resume}));
 await waitFor(()=>expect(mocks.resume).toHaveBeenCalledTimes(2));
 await act(async()=>finishOld({asset_version_id:id(60)}));
 expect(oldApplied).not.toHaveBeenCalled();expect(newApplied).not.toHaveBeenCalled();
 expect(screen.getByRole('button',{name:en.perspective_apply_resume}).hasAttribute('disabled')).toBe(true);
 await act(async()=>finishNew({asset_version_id:id(61)}));
 expect(newApplied).toHaveBeenCalledWith({asset_version_id:id(61)});
});

 it('late clear from old scope cannot remove or unlock the new recovered attempt',async()=>{
 const record={...scoped,selection:null,applied:{asset_version_id:id(60)},selected:false};
 mocks.read.mockResolvedValue(record);
 let finishClear!:()=>void,finishResume!:(value:unknown)=>void;
 mocks.clear.mockImplementationOnce(()=>new Promise<void>(resolve=>{finishClear=resolve;}));
 mocks.resume.mockImplementationOnce(()=>new Promise(resolve=>{finishResume=resolve;}));
 const onApplied=vi.fn();
 const mounted=render(<MultiApplyRecovery {...props} onApplied={onApplied}/>);
 fireEvent.click(await screen.findByText(en.perspective_apply_clear_completed));
 await waitFor(()=>expect(mocks.clear).toHaveBeenCalledTimes(1));
 mounted.rerender(<MultiApplyRecovery {...props} tenant={id(90)} onApplied={onApplied}/>);
 await waitFor(()=>expect(mocks.read).toHaveBeenCalledTimes(2));
 await waitFor(()=>expect(screen.getByRole('button',{name:en.perspective_apply_resume}).hasAttribute('disabled')).toBe(false));
 fireEvent.click(screen.getByRole('button',{name:en.perspective_apply_resume}));
 await waitFor(()=>expect(mocks.resume).toHaveBeenCalledTimes(1));
 await act(async()=>finishClear());
 expect(screen.getByRole('button',{name:en.perspective_apply_resume}).hasAttribute('disabled')).toBe(true);
 expect(onApplied).not.toHaveBeenCalled();
 await act(async()=>finishResume({asset_version_id:id(61)}));
 expect(onApplied).toHaveBeenCalledWith({asset_version_id:id(61)});
 });
