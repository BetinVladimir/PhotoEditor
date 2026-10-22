// @vitest-environment jsdom
import {expectEnabled,clickWhenEnabled} from './test/ui-readiness';
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import type {OwnerAPI} from './owner-api';
import type {PerspectiveAttemptStore} from './perspective-attempt-store';
import type {PerspectiveIntent} from './perspective-intent';
const memory=vi.hoisted(()=>({values:new Map<string,string>(),wait:null as Promise<void>|null,writing:false}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(k:string)=>memory.values.get(k)??null,setItem:async(k:string,v:string)=>{memory.writing=true;if(memory.wait)await memory.wait;memory.values.set(k,v)},removeItem:async(k:string)=>{memory.values.delete(k)}}}));
vi.mock('./semantic-attempt-store',()=>({semanticJournalLock:async(_origin:string,action:()=>Promise<unknown>)=>action()}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:string)=>key})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('./perspective-lines-canvas',()=>({PerspectiveLinesCanvas:()=>null}));
import {EditorPerspectiveLines} from './editor-perspective-lines';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const body:PerspectiveIntent={schema_version:1,snapshot_id:id(1),params:{schema_version:1,base:{asset_version_id:id(2),source:{artifact_id:id(3),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)},strength:1,mode:'manual',corners:[{x:0,y:0},{x:1,y:0},{x:1,y:1},{x:0,y:1}],lines:null,max_crop_fraction:.5}};
function fixture(){
 const api={me:vi.fn(async()=>({id:id(4)})),get:vi.fn(),perspectiveDetectedLines:vi.fn(),submitPerspectiveLineDetection:vi.fn(),createRenderInputSnapshot:vi.fn()} as unknown as OwnerAPI;
 return {api,origin:'https://detector.test',tenant:id(5),project:id(6),version:id(2),body,jobs:{} as PerspectiveAttemptStore,disabled:false,onSnapshot:vi.fn(),onPrepared:vi.fn(),onPending:vi.fn()};
}
afterEach(()=>{cleanup();memory.values.clear();memory.wait=null;memory.writing=false});
it('keeps manual lines editable while an unavailable detector is disabled',async()=>{
 const props=fixture();render(<EditorPerspectiveLines {...props} detectionAvailable={false}/>);
 await waitFor(()=>expect(screen.getByLabelText('horizontal 1 a x').getAttribute('readonly')).toBeNull());
 const detect=screen.getByRole('button',{name:'perspective_lines_detect'});
 expect(detect.getAttribute('aria-disabled')).toBe('true');fireEvent.click(detect);
 expect(props.api.submitPerspectiveLineDetection).not.toHaveBeenCalled();
});
it('does not POST after capability is revoked while persisting the original intent',async()=>{
 const props=fixture(),view=render(<EditorPerspectiveLines {...props} detectionAvailable/>);
 const detect=screen.getByRole('button',{name:'perspective_lines_detect'});
 await waitFor(()=>expectEnabled(detect));
 let release!:()=>void;memory.wait=new Promise<void>(resolve=>{release=resolve});
 await clickWhenEnabled(detect);await waitFor(()=>expect(memory.writing).toBe(true));
 view.rerender(<EditorPerspectiveLines {...props} detectionAvailable={false}/>);
 await act(async()=>{release();});await screen.findByRole('alert');
 expect(props.api.submitPerspectiveLineDetection).not.toHaveBeenCalled();expect(props.api.createRenderInputSnapshot).not.toHaveBeenCalled();expect(props.onPrepared).not.toHaveBeenCalled();
 expect([...memory.values.values()].some(raw=>JSON.parse(raw).body?.snapshot_id===body.snapshot_id)).toBe(true);
});

it('does not POST on live capability loss during persistence without rerender',async()=>{
 let available=true;const props=fixture();render(<EditorPerspectiveLines {...props} detectionAvailable operationAvailable={()=>available}/>);
 const detect=screen.getByRole('button',{name:'perspective_lines_detect'});
 await waitFor(()=>expectEnabled(detect));
 let release!:()=>void;memory.wait=new Promise<void>(resolve=>{release=resolve});
 await clickWhenEnabled(detect);await waitFor(()=>expect(memory.writing).toBe(true));
 available=false;
 await act(async()=>{release();});await screen.findByRole('alert');
 expect(props.api.submitPerspectiveLineDetection).not.toHaveBeenCalled();expect(props.api.createRenderInputSnapshot).not.toHaveBeenCalled();expect(props.onPrepared).not.toHaveBeenCalled();
 expect([...memory.values.values()].some(raw=>JSON.parse(raw).body?.snapshot_id===body.snapshot_id)).toBe(true);
});
