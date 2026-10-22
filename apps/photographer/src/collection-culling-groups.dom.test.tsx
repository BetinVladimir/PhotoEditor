// @vitest-environment jsdom
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
const state=vi.hoisted(()=>({push:vi.fn(),denials:[] as Array<()=>void>}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./localized-router',()=>({router:{push:state.push}}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({locale:'en'})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>({active:true,generation:0})}));
vi.mock('./scene-frame-preview',()=>({SceneFramePreview:({frame,onAccessDenied}:any)=>{state.denials.push(onAccessDenied);return <span>{frame.versionId}</span>}}));
import {CollectionCullingGroups} from './collection-culling-groups';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const target={collection_item_id:id(4),item_id:id(5),asset_id:id(6),asset_version_id:id(7),scene_id:'scene',exact_group_id:'duplicate',burst_group_id:'burst',rank:1,score:.8,recommended:true,manual:false,eye_review:false,eye_state:'open'};
const context={review_id:id(8),run_id:id(9),candidate_hash:'a'.repeat(64),state:'pending',total:2,matched:1,unmatched:1};
const collection={id:id(3),tenant_id:id(1),project_id:id(2),revision:'2'} as any;
const members=[{id:id(4),asset_id:id(6),asset_version_id:id(7),position:0}] as any;
afterEach(()=>{cleanup();state.push.mockReset();state.denials=[]});
it('shows multiple real analyses, historical count and exact member preview before opening its real review',async()=>{
 const request=vi.fn(async(path:string)=>({collection_id:id(3),revision:'2',contexts:path.includes('review_id=')?[{...context,items:[target]}]:[{...context,items:null},{...context,review_id:id(10),items:null}],next_cursor:null}));render(<CollectionCullingGroups client={{request} as any} tenant={id(1)} collection={collection} items={members} current={()=>true}/>);fireEvent.click(await screen.findByText('Analysis 1 · 1/2 · Awaiting selection'));await screen.findByText(id(7));expect(screen.getByText(/Historical frames absent/).textContent).toContain('1');fireEvent.click(screen.getByText('Duplicates'));fireEvent.click(screen.getByText('Open photographer selection'));expect(state.push).toHaveBeenCalledWith({pathname:'/reviews/[id]',params:{id:id(8),tenant:id(1)}});
});
it('a replaced connection refuses a late old analysis and exposes the new explicit empty state',async()=>{
 let finish!:(value:any)=>void;const old={request:vi.fn(()=>new Promise(resolve=>finish=resolve))},fresh={request:vi.fn().mockResolvedValue({collection_id:id(3),revision:'2',contexts:[],next_cursor:null})};const view=render(<CollectionCullingGroups client={old as any} tenant={id(1)} collection={collection} items={members} current={()=>true}/>);await waitFor(()=>expect(old.request).toHaveBeenCalled());view.rerender(<CollectionCullingGroups client={fresh as any} tenant={id(1)} collection={collection} items={members} current={()=>true}/>);await screen.findByText(/No selection analysis/);await act(async()=>finish({collection_id:id(3),revision:'2',contexts:[{...context,items:null}],next_cursor:null}));expect(screen.queryByText('Analysis 1 · 1/2 · Awaiting selection')).toBeNull();expect(state.push).not.toHaveBeenCalled();
});

it('a retained old preview denial cannot cancel the new selected analysis or discovery',async()=>{
 const request=vi.fn(async(path:string)=>({collection_id:id(3),revision:'2',contexts:path.includes('review_id=')?[{...context,review_id:path.includes(id(10))?id(10):id(8),items:[target]}]:[{...context,items:null},{...context,review_id:id(10),items:null}],next_cursor:null}));
 render(<CollectionCullingGroups client={{request} as any} tenant={id(1)} collection={collection} items={members} current={()=>true}/>);
 fireEvent.click(await screen.findByText('Analysis 1 · 1/2 · Awaiting selection'));await screen.findByText(id(7));const denyOld=state.denials.at(-1)!;
 fireEvent.click(screen.getByText('Analysis 2 · 1/2 · Awaiting selection'));await waitFor(()=>expect(request).toHaveBeenCalledWith(expect.stringContaining('review_id='+id(10)),expect.anything()));await screen.findByText(id(7));
 act(()=>denyOld());expect(screen.queryByText('Retry group discovery')).toBeNull();fireEvent.click(screen.getByText('Open photographer selection'));expect(state.push).toHaveBeenLastCalledWith({pathname:'/reviews/[id]',params:{id:id(10),tenant:id(1)}});
});
