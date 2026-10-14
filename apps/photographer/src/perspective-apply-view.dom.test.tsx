// @vitest-environment jsdom
import {afterEach,it,expect,vi} from 'vitest';
import {render,screen,fireEvent,waitFor,cleanup} from '@testing-library/react';
import type {OwnerAPI} from './owner-api';
import type {PerspectiveProposal} from './perspective-proposal';
import type {PerspectiveQuality} from './perspective-quality';
import en from '../../../locales/en/common.json';
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('@react-native-async-storage/async-storage',()=>{const values=new Map<string,string>();return {default:{getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v);},removeItem:async(k:string)=>{values.delete(k);}}};});
vi.mock('./locale-context',()=>({useLocale:()=>({t:(key:keyof typeof en)=>en[key]})}));
import {PerspectiveApplyView} from './perspective-apply-view';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const proposal:PerspectiveProposal={schema_version:1,tenant_id:id(1),project_id:id(2),producer_task_id:id(3),result_id:id(4),result:ref(5),base:{asset_version_id:id(6),source:ref(7),canvas_hash:'b'.repeat(64)},working_image:ref(8),changed_mask:ref(9),report:ref(10),canvas:{width:100,height:80,orientation:1,space_id:'perspective:canvas',source_sha256:'c'.repeat(64)},status:'review',requires_review:true};

function fixture():PerspectiveQuality{return {proposal:structuredClone(proposal),quality_run_id:id(20),quality_task_id:id(21),quality_result_id:id(22),quality_report:ref(23),quality:{target:ref(8),target_kind:'working',stage:'master',threshold_profile_id:'master_v1',status:'pass',reference:null,reasons:[],technical:{below_zero_fraction:0,above_one_fraction:0,clipped_black_fraction:0,clipped_white_fraction:0,sharpness_laplacian:.01,seam_p99_delta:0,finite:true}}};}

afterEach(cleanup);
it('requires confirmation before dispatch and reports exact applied response',async()=>{
 const result={asset_version_id:id(30),parent_asset_version_id:id(6),source:ref(8)},applyPerspective=vi.fn().mockResolvedValue(result),onApplied=vi.fn();
 render(<PerspectiveApplyView origin={'https://studio.test'} api={{applyPerspective,me:async()=>({id:id(50)})} as unknown as OwnerAPI} review={fixture()} onApplied={onApplied}/>);
 const button=screen.getByText(en.perspective_apply_submit).closest('[role="button"]')!;
 expect(button.getAttribute('aria-disabled')).toBe('true');
 fireEvent.click(screen.getByRole('checkbox'));fireEvent.click(button);
 await waitFor(()=>expect(onApplied).toHaveBeenCalledWith(result));
 expect(applyPerspective).toHaveBeenCalledTimes(1);
});
it('never dispatches quality fail',()=>{
 const review=fixture();review.quality.status='fail';const applyPerspective=vi.fn();
 render(<PerspectiveApplyView origin={'https://studio.test'} api={{applyPerspective,me:async()=>({id:id(50)})} as unknown as OwnerAPI} review={review} onApplied={vi.fn()}/>);
 fireEvent.click(screen.getByRole('checkbox'));fireEvent.click(screen.getByText(en.perspective_apply_submit));
 expect(applyPerspective).not.toHaveBeenCalled();
});
