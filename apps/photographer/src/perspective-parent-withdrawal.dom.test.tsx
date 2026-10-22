// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
vi.mock('./localized-router',()=>({router:{push:vi.fn()},useFocusEffect:()=>{}}));
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:string)=>key})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>({active:true,generation:0})}));
vi.mock('./perspective-apply-recovery',()=>({PerspectiveApplyRecovery:({onAccessDenied}:{onAccessDenied:()=>void})=><button onClick={onAccessDenied}>Withdraw recovery</button>}));
vi.mock('./perspective-quality-view',()=>({PerspectiveQualityView:({onAccessDenied}:{onAccessDenied:()=>void})=><button onClick={onAccessDenied}>Withdraw quality</button>}));
vi.mock('./perspective-preview-view',()=>({PerspectivePreviewView:()=> <div>Verified preview</div>}));
vi.mock('./editor-perspective-lines',()=>({EditorPerspectiveLines:()=>null}));
vi.mock('./perspective-canvas',()=>({PerspectiveCanvas:()=>null}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async()=>null,setItem:async()=>{},removeItem:vi.fn()}}));
vi.mock('./perspective-replacement-result',()=>({readPerspectiveReplacementResult:async()=>({state:'available',proposal:{canvas:{width:100,height:100}}})}));
import {EditorPerspective} from './editor-perspective';
import {PerspectiveReplacementReview} from './perspective-replacement-review';
import type {OwnerAPI} from './owner-api';
afterEach(cleanup);
it('withdraws the Editor scope and ignores a late source callback after recovery denial',async()=>{
 let finish!:(v:unknown)=>void;const v2Binding=vi.fn(()=>new Promise(r=>{finish=r;})),createRenderInputSnapshot=vi.fn();
 const api={me:async()=>({id:'actor'}),v2Binding,createRenderInputSnapshot} as unknown as OwnerAPI;
 render(<EditorPerspective api={api} origin="https://photo.test" tenant="tenant" project="project" asset="asset" version="version" onApplied={vi.fn()} onSubmitted={vi.fn()}/>);
 await waitFor(()=>expect(v2Binding).toHaveBeenCalledOnce());fireEvent.click(screen.getByText('Withdraw recovery'));await screen.findByText('Access is no longer available.');
 await act(async()=>finish({asset_version_id:'version'}));expect(createRenderInputSnapshot).not.toHaveBeenCalled();expect(screen.queryByText('perspective_submit')).toBeNull();expect(screen.queryByText('Withdraw recovery')).toBeNull();
});
it('withdraws replacement preview, Apply recovery and refresh after quality denial',async()=>{
 const perspectiveReplacementScope=vi.fn().mockResolvedValue({tenant:'tenant',task:'task',base:{asset_version_id:'version'}});
 render(<PerspectiveReplacementReview api={{perspectiveReplacementScope} as unknown as OwnerAPI} origin="https://photo.test" tenant="tenant" project="project" review="review" originalTask="original" decisionHash="hash"/>);
 await screen.findByText('Verified preview');fireEvent.click(screen.getByText('Withdraw quality'));await screen.findByText('Access is no longer available.');expect(screen.queryByText('Verified preview')).toBeNull();expect(screen.queryByText('Withdraw recovery')).toBeNull();expect(screen.queryByText('perspective_quality_refresh')).toBeNull();expect(perspectiveReplacementScope).toHaveBeenCalledOnce();
});
