// @vitest-environment jsdom
import React from 'react';
import {it,expect,vi,afterEach} from 'vitest';
import {render,screen,waitFor,cleanup,act,fireEvent} from '@testing-library/react';
import type {OwnerAPI} from './owner-api';
import {MultiReplacementReview} from './multi-replacement-review';
import {readMultiReplacementResult} from './multi-replacement-result';
const recovery=vi.hoisted(()=>({applied:false}));
vi.mock('./locale-context',()=>({useLocale:()=>({t:(key:string)=>key})}));
vi.mock('react-native',()=>({View:({children}:any)=><div>{children}</div>,Text:({children,accessibilityRole}:any)=><span role={accessibilityRole}>{children}</span>,Pressable:({children,onPress}:any)=><button onClick={onPress}>{children}</button>}));
vi.mock('./multi-preview-view',()=>({MultiPreviewView:()=>null}));
vi.mock('./multi-quality-view',()=>({MultiQualityView:()=>null}));
vi.mock('./multi-replacement-result',()=>({readMultiReplacementResult:vi.fn().mockResolvedValue({state:'pending',workflow_state:'running'})}));
vi.mock('./multi-apply-recovery',()=>({MultiApplyRecovery:({onApplied}:any)=>recovery.applied?<button onClick={()=>onApplied({asset_version_id:'saved-version'})}>Recover applied</button>:null}));
afterEach(()=>{cleanup();vi.useRealTimers();vi.clearAllMocks();recovery.applied=false;vi.mocked(readMultiReplacementResult).mockResolvedValue({state:'pending',workflow_state:'running'});});
const props={origin:'https://example.test',tenant:'tenant',project:'project',review:'review',originalTask:'original',decisionHash:'hash',operation:'optical.panorama' as const};
it('discovers current scope before observing replacement result',async()=>{
 const api={multiReplacementScope:vi.fn().mockResolvedValue({tenant:'tenant',task:'task',intent:{}})} as unknown as OwnerAPI;
 render(<MultiReplacementReview {...props} api={api}/>);
 await screen.findByText('multi_review_pending');
 expect(api.multiReplacementScope).toHaveBeenCalledWith('optical.panorama','project','review','original','hash',expect.any(AbortSignal));
});
it('discards old client discovery and aborts it on scope change',async()=>{
 let finish!:(value:unknown)=>void;
 const first={multiReplacementScope:vi.fn().mockImplementation(()=>new Promise(resolve=>{finish=resolve;}))} as unknown as OwnerAPI;
 const next={multiReplacementScope:vi.fn().mockRejectedValue(new Error('revoked'))} as unknown as OwnerAPI;
 const view=render(<MultiReplacementReview {...props} api={first}/>);
 await waitFor(()=>expect(first.multiReplacementScope).toHaveBeenCalledTimes(1));
 const signal=(first.multiReplacementScope as any).mock.calls[0][5] as AbortSignal;
 view.rerender(<MultiReplacementReview {...props} api={next}/>);
 expect(signal.aborted).toBe(true);finish({tenant:'tenant',task:'task',intent:{}});
 await screen.findByRole('alert');expect(screen.queryByText('multi_review_pending')).toBeNull();
});

it('polls pending replacement and stops after terminal cancellation',async()=>{
 vi.useFakeTimers();
 const api={multiReplacementScope:vi.fn().mockResolvedValue({tenant:'tenant',task:'task',intent:{}})} as unknown as OwnerAPI;
 vi.mocked(readMultiReplacementResult).mockResolvedValueOnce({state:'pending',workflow_state:'cancelling'})
  .mockResolvedValueOnce({state:'terminal',workflow_state:'cancelled'});
 render(<MultiReplacementReview {...props} api={api}/>);await act(async()=>{});
 expect(screen.getByText('multi_review_cancelling')).toBeTruthy();
 await act(async()=>{vi.advanceTimersByTime(2000);});
 expect(screen.getByRole('alert').textContent).toBe('multi_review_cancelled');
 await act(async()=>{vi.advanceTimersByTime(10000);});expect(api.multiReplacementScope).toHaveBeenCalledTimes(2);
});
it('retains recovered Apply acknowledgement across refresh and stops pending polling',async()=>{
 vi.useFakeTimers();recovery.applied=true;
 const api={multiReplacementScope:vi.fn().mockResolvedValue({tenant:'tenant',task:'task',intent:{}})} as unknown as OwnerAPI;
 render(<MultiReplacementReview {...props} api={api}/>);await act(async()=>{});
 fireEvent.click(screen.getByRole('button',{name:'Recover applied'}));
 expect(screen.getByText('saved-version')).toBeTruthy();
 await act(async()=>{vi.advanceTimersByTime(10000);});expect(api.multiReplacementScope).toHaveBeenCalledTimes(1);
 fireEvent.click(screen.getByRole('button',{name:'multi_review_refresh'}));await act(async()=>{});
 expect(screen.getByText('saved-version')).toBeTruthy();expect(screen.queryByRole('button',{name:'Recover applied'})).toBeNull();
});
