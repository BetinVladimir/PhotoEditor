// @vitest-environment jsdom
import {afterEach,it,expect,vi} from 'vitest';
import {act,render as renderUI,screen,fireEvent,waitFor,cleanup} from '@testing-library/react';
import type {OwnerAPI} from './owner-api';
function render(ui:any){if(!ui.props.api.reconcileCollectionVersion)ui.props.api.reconcileCollectionVersion=vi.fn().mockRejectedValue(new Error('Unexpected collection reconciliation'));return renderUI(ui);}
import type {PerspectiveProposal} from './perspective-proposal';
import {perspectiveQualityTask,type PerspectiveQuality} from './perspective-quality';
import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import en from '../../../locales/en/common.json';
const lifecycle=vi.hoisted(()=>({listeners:new Set<(state:string)=>void>()}));
vi.mock('react-native',async()=>({...await import('react-native-web'),AppState:{currentState:'active',addEventListener:(_event:string,listener:(state:string)=>void)=>{lifecycle.listeners.add(listener);return{remove:()=>lifecycle.listeners.delete(listener)};}}}));
import {APIError} from '@photoeditor/api-client';
vi.mock('@react-native-async-storage/async-storage',()=>{const values=new Map<string,string>();return {default:{getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v);},removeItem:async(k:string)=>{values.delete(k);}}};});
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:keyof typeof en)=>en[key]})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
import {PerspectiveApplyView} from './perspective-apply-view';
import {PerspectiveApplyStore} from './perspective-apply-store';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const proposal:PerspectiveProposal={schema_version:1,tenant_id:id(1),project_id:id(2),producer_task_id:id(3),result_id:id(4),result:ref(5),base:{asset_version_id:id(6),source:ref(7),canvas_hash:'b'.repeat(64)},working_image:ref(8),changed_mask:ref(9),report:ref(10),canvas:{width:100,height:80,orientation:1,space_id:'perspective:canvas',source_sha256:'c'.repeat(64)},status:'review',requires_review:true};

function fixture():PerspectiveQuality{const value:PerspectiveQuality={proposal:structuredClone(proposal),quality_run_id:id(20),quality_task_id:perspectiveQualityTask(id(20)),quality_result_id:id(22),quality_report:ref(23),quality:{target:ref(8),target_kind:'working',stage:'master',threshold_profile_id:'master_v1',status:'pass',reference:null,reasons:[],technical:{below_zero_fraction:0,above_one_fraction:0,clipped_black_fraction:0,clipped_white_fraction:0,sharpness_laplacian:.01,seam_p99_delta:0,finite:true}}};value.quality_report.sha256=bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(value.quality))));return value;}

afterEach(cleanup);
it('withdraws parent Apply projection on access denial',async()=>{
 const onAccessDenied=vi.fn(),onApplied=vi.fn();render(<PerspectiveApplyView origin="https://withdraw-perspective.test" api={{me:async()=>({id:id(50)}),applyPerspective:async()=>{throw new APIError(403,'FORBIDDEN')}} as unknown as OwnerAPI} review={fixture()} onApplied={onApplied} onAccessDenied={onAccessDenied}/>);
 fireEvent.click(screen.getByRole('checkbox'));fireEvent.click(screen.getByText(en.perspective_apply_submit));await waitFor(()=>expect(onAccessDenied).toHaveBeenCalledOnce());expect(onApplied).not.toHaveBeenCalled();
});
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

it('suppresses applied navigation after the actor changes',async()=>{
 const onApplied=vi.fn(),api={me:vi.fn().mockResolvedValueOnce({id:id(50)}).mockResolvedValueOnce({id:id(51)}),applyPerspective:vi.fn().mockResolvedValue({asset_version_id:id(30),parent_asset_version_id:id(6),source:ref(8)})} as unknown as OwnerAPI;
 render(<PerspectiveApplyView api={api} origin="https://actor.test" review={fixture()} onApplied={onApplied}/>);
 fireEvent.click(screen.getByRole('checkbox'));fireEvent.click(screen.getByText(en.perspective_apply_submit));
 await screen.findByText(en.perspective_apply_failed);expect(onApplied).not.toHaveBeenCalled();
 expect(screen.getByText(en.perspective_apply_submit).closest('[role="button"]')?.getAttribute('aria-disabled')).toBe('true');
});
it.each([401,403,404,410])('blocks further submission after definitive denial %s',async status=>{
 const applyPerspective=vi.fn().mockRejectedValue(new APIError(status,'FORBIDDEN'));
 render(<PerspectiveApplyView api={{me:async()=>({id:id(50)}),applyPerspective} as unknown as OwnerAPI} origin={`https://deny-${status}.test`} review={fixture()} onApplied={vi.fn()}/>);
 fireEvent.click(screen.getByRole('checkbox'));fireEvent.click(screen.getByText(en.perspective_apply_submit));await screen.findByText(en.perspective_apply_failed);
 fireEvent.click(screen.getByText(en.perspective_apply_submit));expect(applyPerspective).toHaveBeenCalledTimes(1);
});
it('does not expose a late accepted result while backgrounded',async()=>{
 let finish!:(value:unknown)=>void;const onApplied=vi.fn(),applyPerspective=vi.fn(()=>new Promise(resolve=>{finish=resolve}));
 render(<PerspectiveApplyView api={{me:async()=>({id:id(50)}),applyPerspective} as unknown as OwnerAPI} origin="https://background.test" review={fixture()} onApplied={onApplied}/>);
 fireEvent.click(screen.getByRole('checkbox'));fireEvent.click(screen.getByText(en.perspective_apply_submit));await waitFor(()=>expect(applyPerspective).toHaveBeenCalled());
 act(()=>{for(const listener of lifecycle.listeners)listener('background');});
 await act(async()=>{finish({asset_version_id:id(30),parent_asset_version_id:id(6),source:ref(8)});});expect(onApplied).not.toHaveBeenCalled();
});

it.each(['proposal','collection'] as const)('retains but refuses a saved %s choice belonging to another displayed result',async(kind)=>{
 const review=fixture(),savedReview=structuredClone(review);
 if(kind==='proposal')savedReview.proposal.producer_task_id=id(99);
 const read=vi.spyOn(PerspectiveApplyStore.prototype,'read').mockResolvedValue({schema_version:1,review:savedReview,warnings:false,selection:kind==='collection'?{collection:id(80),item:id(81),asset:id(82),revision:'1',expectedVersion:id(6)}:null,applied:null,selected:false});
 const write=vi.spyOn(PerspectiveApplyStore.prototype,'write'),applyPerspective=vi.fn(),onApplied=vi.fn();
 try{
  render(<PerspectiveApplyView api={{me:async()=>({id:id(50)}),applyPerspective} as unknown as OwnerAPI} origin="https://saved-scope.test" review={review} onApplied={onApplied}/>);
  fireEvent.click(screen.getByRole('checkbox'));fireEvent.click(screen.getByText(en.perspective_apply_submit));
  await screen.findByText(en.perspective_apply_failed);expect(applyPerspective).not.toHaveBeenCalled();expect(write).not.toHaveBeenCalled();expect(onApplied).not.toHaveBeenCalled();
  expect(screen.getByText(en.perspective_apply_submit).closest('[role="button"]')?.getAttribute('aria-disabled')).toBe('true');
 }finally{read.mockRestore();write.mockRestore();}
});
