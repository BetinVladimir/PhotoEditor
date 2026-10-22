// @vitest-environment jsdom
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
const state=vi.hoisted(()=>({rows:new Map<string,string>(),buttons:new Map<string,()=>void>(),inputs:new Map<string,(value:string)=>void>()}));
vi.mock('react-native-paper',async()=>({...await vi.importActual<typeof import('react-native-paper')>('react-native-paper'),View:({children}:any)=><div>{children}</div>,Text:({children}:any)=><span>{children}</span>,TextInput:({value,onChangeText,accessibilityLabel,editable}:any)=>{state.inputs.set(accessibilityLabel,onChangeText);return <input aria-label={accessibilityLabel} value={value} disabled={!editable} onChange={event=>onChangeText(event.target.value)}/>},Button:({children,onPress,disabled}:any)=>{const name=children;if(typeof name==='string')state.buttons.set(name,onPress);return <button disabled={disabled} onClick={onPress}>{children}</button>}}));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>({active:true,generation:0})}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({locale:'en'})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('./semantic-attempt-store',()=>({semanticJournalLock:async(_origin:string,fn:()=>Promise<unknown>)=>fn()}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(key:string)=>state.rows.get(key)??null,setItem:async(key:string,value:string)=>{state.rows.set(key,value)},removeItem:async(key:string)=>{state.rows.delete(key)},getAllKeys:async()=>[...state.rows.keys()]}}));
import {CollectionBulkControls} from './collection-bulk-controls';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
beforeEach(()=>{state.rows.clear();state.buttons.clear();state.inputs.clear()});afterEach(cleanup);
it('retained Apply freezes the latest draft before actor await and retained edit handlers cannot change the pending command',async()=>{
 let actorCalls=0,resolveActor!:(value:unknown)=>void;
 const request=vi.fn(async(path:string):Promise<unknown>=>{if(path==='/api/v1/me'){actorCalls++;if(actorCalls===2)return new Promise(resolve=>{resolveActor=resolve});return{id:id(1)}}throw new Error('network unavailable')});
 const props={client:{request} as never,origin:'https://bulk-stale.test',tenant:id(2),collection:{id:id(3),project_id:id(4),revision:'3',state:'draft'} as never,items:[{id:id(5),position:0,rating:0,flag:'none',tags:[]}] as never,canEdit:true,current:()=>true,onChanged:vi.fn(),acquire:()=>Symbol(),release:vi.fn()};
 render(<CollectionBulkControls {...props}/>);await waitFor(()=>expect(screen.getByRole('button',{name:'Select all'}).hasAttribute('disabled')).toBe(false));
 fireEvent.click(screen.getByText('Select all'));const oldApply=state.buttons.get('Apply to selected')!;
 fireEvent.click(screen.getByText('Tag'));const oldEdit=state.inputs.get('Tag')!;fireEvent.change(screen.getByLabelText('Tag'),{target:{value:'latest tag'}});
 // Invoke the actual earlier render callback, bypassing disabled native UI.
 act(()=>oldApply());await waitFor(()=>expect(resolveActor).toBeTypeOf('function'));
 act(()=>{oldEdit('late stale edit');state.buttons.get('Clear selection')!();state.buttons.get('Rating')!()});
 expect(screen.getByLabelText('Tag')).toHaveProperty('value','latest tag');
 await act(async()=>resolveActor({id:id(1)}));await screen.findByText('Unable to confirm. Retry preserves the same selection and request.');
 const saved=JSON.parse([...state.rows.entries()].find(([key])=>key.startsWith('photo.collection-bulk.v1:'))![1]);
 expect(saved.command).toMatchObject({revision:'3',item_ids:[id(5)],action:'tag',params:{tag:'latest tag',mode:'add'}});
 expect(request.mock.calls.filter(([path])=>path.endsWith('/selection-snapshots'))).toHaveLength(1);
});
