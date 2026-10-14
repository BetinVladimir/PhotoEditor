// @vitest-environment jsdom
import {afterEach,it,expect,vi} from 'vitest';
import {render,screen,fireEvent,waitFor,cleanup,act} from '@testing-library/react';
import type {OwnerAPI} from './owner-api';
import type {MultiProposal} from './multi-proposal';
import type {MultiQuality} from './multi-quality';
import type {MultiIntent,MultiOperation} from './multi-intent';
import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import en from '../../../locales/en/common.json';
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('@react-native-async-storage/async-storage',()=>{const values=new Map<string,string>();return {default:{getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v);},removeItem:async(k:string)=>{values.delete(k);}}};});
vi.mock('./locale-context',()=>({useLocale:()=>({t:(key:keyof typeof en)=>en[key]})}));
import {MultiApplyView} from './multi-apply-view';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
function scopeFixture(op:MultiOperation){
 const frames=[{asset_version_id:id(20),source:ref(21),canvas_hash:'b'.repeat(64)},{asset_version_id:id(22),source:ref(23),canvas_hash:'c'.repeat(64)}];
 const common={schema_version:1 as const,frames,reference_ordinal:1,max_pixels:10000,calibration:ref(24)};
 const intent:MultiIntent={schema_version:1,snapshot_id:id(25),params:op==='optical.panorama'?{...common,projection:'planar'}:{...common,motion_policy:'review'}};
 const proposal:MultiProposal={schema_version:1,tenant_id:id(1),project_id:id(2),producer_task_id:id(3),operation:op,result_id:id(4),result:ref(5),sources:structuredClone(frames),reference_ordinal:1,working_image:ref(6),changed_mask:ref(7),report:ref(8),registrations:[ref(9),ref(10)],canvas:{width:100,height:100,orientation:1,space_id:'multi:'+'d'.repeat(32),source_sha256:'e'.repeat(64)},status:'review',requires_review:true,reason:op.slice(8).toUpperCase()+'_REVIEW;motion_fraction=0.125;REGISTRATION_INCONSISTENT'};
 return {intent,proposal};
}

function quality(proposal:MultiProposal):MultiQuality{const value:MultiQuality={proposal:structuredClone(proposal),quality_run_id:id(40),quality_task_id:id(41),quality_result_id:id(42),quality_report:ref(43),quality:{target:proposal.working_image,target_kind:'working',stage:'master',threshold_profile_id:'master_v1',status:'pass',reference:null,reasons:[],technical:{below_zero_fraction:0,above_one_fraction:0,clipped_black_fraction:0,clipped_white_fraction:0,sharpness_laplacian:.01,seam_p99_delta:0,finite:true}}};seal(value);return value;}
function seal(v:MultiQuality){v.quality_report.sha256=bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(v.quality))));}

function fixture(){return quality(scopeFixture("optical.panorama").proposal);}
const intent=()=>scopeFixture("optical.panorama").intent;
afterEach(cleanup);
it('requires confirmation before dispatch and reports exact applied response',async()=>{
 const result={asset_version_id:id(30),parent_asset_version_id:id(22),source:ref(6)},applyMulti=vi.fn().mockResolvedValue(result),onApplied=vi.fn();
 render(<MultiApplyView origin={'https://studio.test'} api={{applyMulti,me:async()=>({id:id(50)})} as unknown as OwnerAPI} intent={intent()} review={fixture()} onApplied={onApplied}/>);
 const button=screen.getByText(en.perspective_apply_submit).closest('[role="button"]')!;
 expect(button.getAttribute('aria-disabled')).toBe('true');
 fireEvent.click(screen.getByRole('checkbox'));fireEvent.click(button);
 await waitFor(()=>expect(onApplied).toHaveBeenCalledWith(result));
 expect(applyMulti).toHaveBeenCalledTimes(1);
});
it('never dispatches quality fail',()=>{
 const review=fixture();review.quality.status='fail';const applyMulti=vi.fn();
 render(<MultiApplyView origin={'https://studio.test'} api={{applyMulti,me:async()=>({id:id(50)})} as unknown as OwnerAPI} intent={intent()} review={review} onApplied={vi.fn()}/>);
 fireEvent.click(screen.getByRole('checkbox'));fireEvent.click(screen.getByText(en.perspective_apply_submit));
 expect(applyMulti).not.toHaveBeenCalled();
});

it('replacing API aborts pending identity lookup and requires fresh confirmation',async()=>{
 let finish!:(value:unknown)=>void,oldSignal:AbortSignal|undefined;
 const oldApply=vi.fn(),nextApply=vi.fn(),onApplied=vi.fn();
 const oldAPI={me:vi.fn((signal?:AbortSignal)=>{oldSignal=signal;return new Promise(resolve=>{finish=resolve;});}),applyMulti:oldApply} as unknown as OwnerAPI;
 const nextAPI={me:vi.fn(),applyMulti:nextApply} as unknown as OwnerAPI;
 const review=fixture(),frozenIntent=intent();
 const mounted=render(<MultiApplyView origin="https://studio.test" api={oldAPI} intent={frozenIntent} review={review} onApplied={onApplied}/>);
 fireEvent.click(screen.getByRole('checkbox'));fireEvent.click(screen.getByText(en.perspective_apply_submit));
 await waitFor(()=>expect(oldAPI.me).toHaveBeenCalledTimes(1));
 mounted.rerender(<MultiApplyView origin="https://studio.test" api={nextAPI} intent={structuredClone(frozenIntent)} review={structuredClone(review)} onApplied={onApplied}/>);
 expect(oldSignal?.aborted).toBe(true);
 expect(screen.getByRole('checkbox').getAttribute('aria-checked')).toBe('false');
 await act(async()=>finish({id:id(90)}));
 expect(oldApply).not.toHaveBeenCalled();expect(nextApply).not.toHaveBeenCalled();expect(onApplied).not.toHaveBeenCalled();
});
it('equivalent frozen props retain confirmation but changed selection resets it',()=>{
 const api={me:vi.fn(),applyMulti:vi.fn()} as unknown as OwnerAPI,review=fixture(),frozenIntent=intent();
 const mounted=render(<MultiApplyView origin="https://studio.test" api={api} intent={frozenIntent} review={review} onApplied={vi.fn()}/>);
 fireEvent.click(screen.getByRole('checkbox'));
 mounted.rerender(<MultiApplyView origin="https://studio.test" api={api} intent={structuredClone(frozenIntent)} review={structuredClone(review)} onApplied={vi.fn()}/>);
 expect(screen.getByRole('checkbox').getAttribute('aria-checked')).toBe('true');
 mounted.rerender(<MultiApplyView origin="https://studio.test" api={api} intent={frozenIntent} review={review} selection={{collection:id(70),item:id(71),asset:id(72),revision:'2',expectedVersion:id(22)}} onApplied={vi.fn()}/>);
 expect(screen.getByRole('checkbox').getAttribute('aria-checked')).toBe('false');
 expect(api.me).not.toHaveBeenCalled();
});
