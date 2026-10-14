// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
const mocks=vi.hoisted(()=>({read:vi.fn()}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./multi-apply-recovery',()=>({MultiApplyRecovery:()=> <div data-testid="multi-recovery"/>}));
vi.mock('./multi-quality-view',()=>({MultiQualityView:()=>null}));
vi.mock('./multi-preview-view',()=>({MultiPreviewView:()=>null}));
vi.mock('./multi-result',()=>({readMultiAttemptResult:mocks.read,multiTicketProducer:()=> '00000000-0000-4000-8000-000000000099'}));
import {MultiReviewView} from './multi-review-view';
import {LocaleProvider} from './locale-context';
import type {OwnerAPI} from './owner-api';
import type {MultiAttemptRecord} from './multi-attempt';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const attempt=():MultiAttemptRecord=>({schema_version:1,operation:'optical.panorama',project:id(2),key:'intent-key',body:{schema_version:1,snapshot_id:id(30),params:{schema_version:1,frames:[{asset_version_id:id(20),source:ref(21),canvas_hash:'b'.repeat(64)},{asset_version_id:id(22),source:ref(23),canvas_hash:'c'.repeat(64)}],reference_ordinal:1,max_pixels:10000,calibration:ref(24),projection:'planar'}},ticket:{run_id:id(4),workflow_id:id(5),state:'queued'}});
const available=()=>({state:'available',proposal:{sources:attempt().body.params.frames,reference_ordinal:1,canvas:{width:100,height:100},reason:'PANORAMA_REVIEW;motion_fraction=0.125;REGISTRATION_INCONSISTENT'}});
const api={} as Pick<OwnerAPI,'get'|'multiProposal'|'multiPreview'|'multiQuality'>;
function view(value=attempt()){return <LocaleProvider><MultiReviewView api={api} tenant={id(1)} attempt={value}/></LocaleProvider>;}
afterEach(()=>{cleanup();vi.useRealTimers();vi.resetAllMocks();});
it('shows current review evidence and keeps refresh read-only',async()=>{
 mocks.read.mockResolvedValue(available());render(view());
 await screen.findByText('Source photos: 2');expect(screen.getByText('Reference frame: 2')).toBeTruthy();expect(screen.getByText('Motion area: 12.50%')).toBeTruthy();expect(screen.getByText('Frame alignment needs careful review.')).toBeTruthy();expect(screen.getByText('The result has not been applied.')).toBeTruthy();
 expect(screen.queryByRole('button',{name:/apply/i})).toBeNull();fireEvent.click(screen.getByRole('button',{name:'Refresh result'}));await waitFor(()=>expect(mocks.read).toHaveBeenCalledTimes(2));
});
it('shows pending then available after refresh',async()=>{
 mocks.read.mockResolvedValueOnce({state:'pending',workflow_state:'running'}).mockResolvedValueOnce(available());render(view());await screen.findByText('Processing is still in progress. Refresh later.');fireEvent.click(screen.getByRole('button',{name:'Refresh result'}));await screen.findByText('Source photos: 2');
});
it('allows retry after a failed read',async()=>{
 mocks.read.mockRejectedValueOnce(new Error('stale')).mockResolvedValueOnce(available());render(view());await screen.findByRole('alert');fireEvent.click(screen.getByRole('button',{name:'Refresh result'}));await screen.findByText('Source photos: 2');expect(screen.queryByRole('alert')).toBeNull();
});
it('aborts replaced scope and never displays its late result',async()=>{
 let finish!:(v:unknown)=>void;mocks.read.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve;})).mockResolvedValueOnce({state:'pending',workflow_state:'running'});
 const mounted=render(view());await waitFor(()=>expect(mocks.read).toHaveBeenCalledTimes(1));const signal=mocks.read.mock.calls[0][6] as AbortSignal;
 const next=attempt();next.ticket={...next.ticket!,run_id:id(40)};mounted.rerender(view(next));await screen.findByText('Processing is still in progress. Refresh later.');expect(signal.aborted).toBe(true);
 await act(async()=>finish(available()));expect(screen.queryByText('Source photos: 2')).toBeNull();
});
it('refuses missing acknowledgement before result IO',async()=>{
 const missing=attempt();missing.ticket=null;render(view(missing));await screen.findByRole('alert');expect(mocks.read).not.toHaveBeenCalled();
});

it('keeps durable Apply recovery mounted when current result has expired',async()=>{
 mocks.read.mockRejectedValue(new Error('expired'));
 render(<LocaleProvider><MultiReviewView api={api} tenant={id(1)} attempt={attempt()} apply={{api:api as OwnerAPI,origin:'https://studio.test',collection:id(50),selection:null,onApplied:vi.fn()}}/></LocaleProvider>);
 await screen.findByRole('alert');expect(screen.getByTestId('multi-recovery')).toBeTruthy();
});
it('does not mount recovery without an acknowledged ticket',async()=>{
 const missing=attempt();missing.ticket=null;
 render(<LocaleProvider><MultiReviewView api={api} tenant={id(1)} attempt={missing} apply={{api:api as OwnerAPI,origin:'https://studio.test',collection:id(50),selection:null,onApplied:vi.fn()}}/></LocaleProvider>);
 await screen.findByRole('alert');expect(screen.queryByTestId('multi-recovery')).toBeNull();
});

it('polls confirmed pending read without overlapping and stops at available',async()=>{
 vi.useFakeTimers();
 let finish!:(v:unknown)=>void;
 mocks.read.mockResolvedValueOnce({state:'pending',workflow_state:'running'})
  .mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve;}));
 render(view());await act(async()=>{});expect(mocks.read).toHaveBeenCalledTimes(1);
 await act(async()=>{vi.advanceTimersByTime(2000);});expect(mocks.read).toHaveBeenCalledTimes(2);
 await act(async()=>{vi.advanceTimersByTime(10000);});expect(mocks.read).toHaveBeenCalledTimes(2);
 await act(async()=>finish(available()));expect(screen.getByText('Source photos: 2')).toBeTruthy();
 await act(async()=>{vi.advanceTimersByTime(10000);});expect(mocks.read).toHaveBeenCalledTimes(2);
});
it('clears pending poll on unmount and does not automatically retry errors',async()=>{
 vi.useFakeTimers();mocks.read.mockResolvedValueOnce({state:'pending',workflow_state:'queued'});
 const mounted=render(view());await act(async()=>{});mounted.unmount();
 await act(async()=>{vi.advanceTimersByTime(10000);});expect(mocks.read).toHaveBeenCalledTimes(1);
 mocks.read.mockRejectedValueOnce(new Error('offline'));render(view());await act(async()=>{});
 expect(screen.getByRole('alert')).toBeTruthy();
 await act(async()=>{vi.advanceTimersByTime(10000);});expect(mocks.read).toHaveBeenCalledTimes(2);
});

it.each(['failed','cancelled'] as const)('shows terminal %s and never polls it',async status=>{
 vi.useFakeTimers();mocks.read.mockResolvedValue({state:'terminal',workflow_state:status});
 render(view());await act(async()=>{});
 expect(screen.getByRole('alert').textContent).toBe(status==='failed'?'Processing failed.':'Processing was cancelled.');
 expect(screen.queryByText('Source photos: 2')).toBeNull();
 await act(async()=>{vi.advanceTimersByTime(10000);});expect(mocks.read).toHaveBeenCalledTimes(1);
});
