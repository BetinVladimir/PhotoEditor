// @vitest-environment jsdom
import React from 'react';
import {it,expect,vi,afterEach} from 'vitest';
vi.mock('./localized-router',()=>({router:{push:vi.fn()},useFocusEffect:()=>{}}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async()=>null,setItem:async()=>{},removeItem:async()=>{},getAllKeys:async()=>[]}}));
import {act,fireEvent,render,screen,waitFor,cleanup} from '@testing-library/react';
import type {OwnerAPI} from './owner-api';
import {PerspectiveReplacementReview} from './perspective-replacement-review';
const recovery=vi.hoisted(()=>({enabled:false}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:string)=>key})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('react-native',async()=>({...await import('react-native-web'),AppState:{currentState:'active',addEventListener:()=>({remove:()=>{}})}}));
vi.mock('./perspective-apply-recovery',()=>({PerspectiveApplyRecovery:({onApplied}:any)=>recovery.enabled?<button onClick={()=>onApplied({asset_version_id:"saved-version"})}>Recover applied</button>:null}));
vi.mock('./perspective-preview-view',()=>({PerspectivePreviewView:()=>null}));
vi.mock('./perspective-quality-view',()=>({PerspectiveQualityView:()=>null}));
vi.mock('./perspective-replacement-result',()=>({readPerspectiveReplacementResult:vi.fn().mockResolvedValue({state:'pending',workflow_state:'running'})}));
afterEach(()=>{cleanup();vi.useRealTimers();recovery.enabled=false;});
const props={origin:'https://example.test',tenant:'tenant',project:'project',review:'review',originalTask:'original',decisionHash:'hash'};
it('discovers current scope before observing replacement result',async()=>{
 const api={perspectiveReplacementScope:vi.fn().mockResolvedValue({tenant:'tenant',task:'task',base:{asset_version_id:'version'}})} as unknown as OwnerAPI;
 render(<PerspectiveReplacementReview {...props} api={api}/>);
 await screen.findByText('perspective_quality_pending');
 expect(api.perspectiveReplacementScope).toHaveBeenCalledWith('project','review','original','hash',expect.any(AbortSignal));
});
it('discards old client discovery and aborts it on scope change',async()=>{
 let finish!:(value:unknown)=>void;
 const first={perspectiveReplacementScope:vi.fn().mockImplementation(()=>new Promise(resolve=>{finish=resolve;}))} as unknown as OwnerAPI;
 const next={perspectiveReplacementScope:vi.fn().mockRejectedValue(new Error('revoked'))} as unknown as OwnerAPI;
 const view=render(<PerspectiveReplacementReview {...props} api={first}/>);
 await waitFor(()=>expect(first.perspectiveReplacementScope).toHaveBeenCalledTimes(1));
 const signal=(first.perspectiveReplacementScope as any).mock.calls[0][4] as AbortSignal;
 view.rerender(<PerspectiveReplacementReview {...props} api={next}/>);
 expect(signal.aborted).toBe(true);finish({tenant:'tenant',task:'task',base:{asset_version_id:'version'}});
 await screen.findByRole('alert');expect(screen.queryByText('perspective_quality_pending')).toBeNull();
});

it('rediscovers authorized scope while replacement is pending and stops on failure',async()=>{
 vi.useFakeTimers();
 const perspectiveReplacementScope=vi.fn().mockResolvedValueOnce({tenant:'tenant',task:'task',base:{asset_version_id:'version'}}).mockRejectedValue(new Error('revoked'));
 const api={perspectiveReplacementScope} as unknown as OwnerAPI;
 render(<PerspectiveReplacementReview {...props} api={api}/>);
 await act(async()=>{});expect(screen.getByText('perspective_quality_pending')).toBeTruthy();
 await act(async()=>vi.advanceTimersByTimeAsync(2000));
 expect(perspectiveReplacementScope).toHaveBeenCalledTimes(2);expect(screen.getByRole('alert')).toBeTruthy();
 await act(async()=>vi.advanceTimersByTimeAsync(10000));expect(perspectiveReplacementScope).toHaveBeenCalledTimes(2);
});
it('cancels pending polling on unmount',async()=>{
 vi.useFakeTimers();const perspectiveReplacementScope=vi.fn().mockResolvedValue({tenant:'tenant',task:'task',base:{asset_version_id:'version'}});
 const view=render(<PerspectiveReplacementReview {...props} api={{perspectiveReplacementScope} as unknown as OwnerAPI}/>);
 await act(async()=>{});view.unmount();await act(async()=>vi.advanceTimersByTimeAsync(5000));
 expect(perspectiveReplacementScope).toHaveBeenCalledTimes(1);
});

it('keeps recovered Apply acknowledgement through refresh without another Apply action',async()=>{
 recovery.enabled=true;const perspectiveReplacementScope=vi.fn().mockResolvedValue({tenant:'tenant',task:'task',base:{asset_version_id:'version'}});
 render(<PerspectiveReplacementReview {...props} api={{perspectiveReplacementScope} as unknown as OwnerAPI}/>);
 fireEvent.click(await screen.findByText('Recover applied'));expect(screen.getByText('saved-version')).toBeTruthy();
 fireEvent.click(screen.getByText('perspective_quality_refresh'));
 await waitFor(()=>expect(perspectiveReplacementScope).toHaveBeenCalledTimes(2));
 expect(screen.getByText('saved-version')).toBeTruthy();expect(screen.queryByText('Recover applied')).toBeNull();
});
