// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen} from '@testing-library/react';
import type {OwnerAPI} from './owner-api';
import type {NegativeIntent} from './negative-intent';
import type {PerspectiveIntent} from './perspective-intent';
const state=vi.hoisted(()=>({values:new Map<string,string>()}));
vi.mock('react-native',async()=>({...await import('react-native-web'),AppState:{currentState:'active',addEventListener:()=>({remove:()=>{}})}}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(k:string)=>state.values.get(k)??null,setItem:async(k:string,v:string)=>{state.values.set(k,v)},removeItem:async(k:string)=>{state.values.delete(k)}}}));
vi.mock('./semantic-attempt-store',()=>({semanticJournalLock:async(_origin:string,action:()=>Promise<unknown>)=>action()}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:string)=>key})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('./perspective-apply-recovery',()=>({PerspectiveApplyRecovery:()=>null}));
vi.mock('./editor-perspective-lines',()=>({EditorPerspectiveLines:()=>null}));
vi.mock('./perspective-canvas',()=>({PerspectiveCanvas:()=>null}));
import {EditorNegative} from './editor-negative';
import {EditorPerspective} from './editor-perspective';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const base={asset_version_id:id(1),source:{artifact_id:id(2),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)};
const negative:NegativeIntent={schema_version:1,snapshot_id:id(3),params:{schema_version:1,base,strength:1,film_base_rgb:[1,1,1],calibration:{artifact_id:id(4),sha256:'c'.repeat(64)},exposure_ev:0}};
const perspective:PerspectiveIntent={schema_version:1,snapshot_id:id(3),params:{schema_version:1,base,strength:1,mode:'manual',corners:[{x:0,y:0},{x:1,y:0},{x:1,y:1},{x:0,y:1}],lines:null,max_crop_fraction:.5}};
afterEach(()=>{cleanup();state.values.clear()});
for(const kind of ['negative','perspective'] as const){
 it(`${kind} keeps the journal and blocks replay when live source SHA changes`,async()=>{
  const origin='https://optical.test',user=id(5),tenant=id(6),project=id(7);
  const key=`photo.${kind}-attempt.v1:`+JSON.stringify([origin,user,tenant,project,base.asset_version_id]);
  const record={schema_version:1,project,key:'original-key',body:kind==='negative'?negative:perspective,ticket:null};
  state.values.set(key,JSON.stringify(record));
  const api={me:vi.fn(async()=>({id:user})),v2Binding:vi.fn(async()=>({...base,source:{...base.source,sha256:'d'.repeat(64)}})),createRenderInputSnapshot:vi.fn(),negativeCapabilities:vi.fn(),submitNegative:vi.fn(),submitPerspective:vi.fn()} as unknown as OwnerAPI;
  const props={api,origin,tenant,project,asset:id(8),version:base.asset_version_id,onSubmitted:vi.fn()};
  if(kind==='negative')render(<EditorNegative {...props}/>);
  else render(<EditorPerspective {...props} url="https://media.test/image" aspect={1} selection={null} onApplied={vi.fn()}/>);
  expect((await screen.findByRole('alert')).textContent).toBe(`${kind}_unavailable`);
  const button=screen.getByRole('button',{name:`${kind}_submit`});
  expect(button.getAttribute('aria-disabled')).toBe('true');fireEvent.click(button);
  expect(api.submitNegative).not.toHaveBeenCalled();expect(api.submitPerspective).not.toHaveBeenCalled();expect(api.createRenderInputSnapshot).not.toHaveBeenCalled();expect(props.onSubmitted).not.toHaveBeenCalled();
  expect(JSON.parse(state.values.get(key)!)).toEqual(record);
 });
}
