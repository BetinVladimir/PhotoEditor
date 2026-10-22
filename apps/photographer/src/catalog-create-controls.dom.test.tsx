// @vitest-environment jsdom
import {expectEnabled,clickWhenEnabled} from './test/ui-readiness';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import en from '../../../locales/en/common.json';
import ru from '../../../locales/ru/common.json';
import bg from '../../../locales/bg/common.json';
const catalogs={en,ru,bg};
const state=vi.hoisted(()=>({storage:new Map<string,string>(),remove:null as null|(()=>Promise<void>),locale:'en' as 'en'|'ru'|'bg'}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>({active:true,generation:0})}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:keyof typeof en)=>catalogs[state.locale][key]})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(k:string)=>state.storage.get(k)??null,setItem:async(k:string,v:string)=>{state.storage.set(k,v)},removeItem:async(k:string)=>{await state.remove?.();state.storage.delete(k)}}}));
import {CatalogCreateControls} from './catalog-create-controls';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
afterEach(cleanup);beforeEach(()=>{state.storage.clear();state.remove=null;state.locale='en';});
async function enabled(name:string){await waitFor(()=>{const button=screen.getByRole('button',{name});expectEnabled(button);expect(button.hasAttribute('disabled')).toBe(false);});}
function fixture(kind:'project'|'style'='project'){
 const row={id:id(4),tenant_id:id(2),name:'Wedding',description:'',revision:'1',created_at:'2026-10-17T10:00:00Z',...(kind==='project'?{visibility:'workspace',download_allowed:true,can_edit:true}:{project_id:id(3),archived:false})},created=vi.fn();
 const request=vi.fn<(path:string,options?:{method?:string;body?:unknown;idempotencyKey?:string;signal?:AbortSignal})=>Promise<unknown>>(async(path,options)=>path==='/api/v1/me'?{id:id(1),email:'fixture@example.test',locale:'en',theme:'system',timezone:'UTC',revision:'1'}:options?.method==='POST'?row:row);
 const client={request};const ui=()=> <CatalogCreateControls client={client as never} origin="https://catalog.test" tenant={id(2)} project={kind==='style'?id(3):null} kind={kind} enabled current={()=>true} onCreated={created}/>;
 return {row,request,created,ui};
}
it.each(['project','style'] as const)('persists %s intent, retries identical command after remount, and acknowledges only exact GET',async kind=>{
 const f=fixture(kind);let lost=true;f.request.mockImplementation(async(path,options)=>{if(path==='/api/v1/me')return {id:id(1)};if(options?.method==='POST'&&lost){lost=false;throw new Error('lost reply')}return f.row;});
 let view=render(f.ui());fireEvent.change(screen.getByLabelText(kind==='project'?'Project name':'Style name'),{target:{value:'Wedding'}});await enabled('Create');
 fireEvent.change(screen.getByLabelText(kind==='project'?'Project name':'Style name'),{target:{value:'Wedding'}});await clickWhenEnabled(screen.getByText('Create'));
 await screen.findByText(en.catalog_create_uncertain);const first=f.request.mock.calls.find(([,options])=>options?.method==='POST');
 view.unmount();view=render(f.ui());await screen.findByDisplayValue('Wedding');await enabled('Recover saved creation');await clickWhenEnabled(screen.getByRole('button',{name:'Recover saved creation'}));
 await waitFor(()=>expect(f.created).toHaveBeenCalledOnce());const posts=f.request.mock.calls.filter(([,options])=>options?.method==='POST');expect(posts).toHaveLength(2);expect(posts[1][0]).toBe(first![0]);expect(posts[1][1]?.body).toEqual(first![1]?.body);expect(posts[1][1]?.idempotencyKey).toBe(first![1]?.idempotencyKey);
});
it('cancelled late POST never publishes and remount recovers the same frozen key',async()=>{
 const f=fixture();let finish!:(value:unknown)=>void;
 f.request.mockImplementation(async(path,options)=>path==='/api/v1/me'?{id:id(1),email:'fixture@example.test',locale:'en',theme:'system',timezone:'UTC',revision:'1'}:options?.method==='POST'?new Promise(resolve=>{finish=resolve}):f.row);
 const view=render(f.ui());fireEvent.change(screen.getByLabelText('Project name'),{target:{value:'Wedding'}});await enabled('Create');
 fireEvent.change(screen.getByLabelText('Project name'),{target:{value:'Wedding'}});await clickWhenEnabled(screen.getByText('Create'));
 await waitFor(()=>expect(f.request.mock.calls.filter(([,o])=>o?.method==='POST')).toHaveLength(1));await clickWhenEnabled(screen.getByText(en.read_cancel));
 await act(async()=>{finish(f.row)});expect(f.created).not.toHaveBeenCalled();view.unmount();render(f.ui());await screen.findByDisplayValue('Wedding');expect(screen.getByText('Recover saved creation')).toBeTruthy();
});
it('an accepted receipt survives GET failure and retries with GET only',async()=>{
 const f=fixture();let reads=0;f.request.mockImplementation(async(path,options)=>{if(path==='/api/v1/me')return{id:id(1),email:'fixture@example.test',locale:'en',theme:'system',timezone:'UTC',revision:'1'};if(options?.method==='POST')return f.row;if(reads++===0)throw new Error('offline');return f.row});
 const view=render(f.ui());fireEvent.change(screen.getByLabelText('Project name'),{target:{value:'Wedding'}});await enabled('Create');
 fireEvent.change(screen.getByLabelText('Project name'),{target:{value:'Wedding'}});await clickWhenEnabled(screen.getByText('Create'));await screen.findByText(en.catalog_create_uncertain);
 view.unmount();render(f.ui());await screen.findByDisplayValue('Wedding');await enabled('Recover saved creation');await clickWhenEnabled(screen.getByRole('button',{name:'Recover saved creation'}));await waitFor(()=>expect(f.created).toHaveBeenCalledOnce());expect(f.request.mock.calls.filter(([,o])=>o?.method==='POST')).toHaveLength(1);
});

it('publishes the validated row before an acknowledgement clear can be cancelled',async()=>{
 const f=fixture();let release!:()=>void;state.remove=()=>new Promise<void>(resolve=>{release=resolve});
 render(f.ui());fireEvent.change(screen.getByLabelText('Project name'),{target:{value:'Wedding'}});await enabled('Create');
 fireEvent.change(screen.getByLabelText('Project name'),{target:{value:'Wedding'}});await clickWhenEnabled(screen.getByText('Create'));
 await waitFor(()=>expect(f.created).toHaveBeenCalledOnce());await clickWhenEnabled(screen.getByText(en.read_cancel));
 await act(async()=>{release()});expect(f.created).toHaveBeenCalledWith(f.row);expect(screen.getByText('Recover saved creation')).toBeTruthy();
 expect(f.request.mock.calls.filter(([,o])=>o?.method==='POST')).toHaveLength(1);
});

it('retained old create cannot regain authority after same-scope parent refresh',async()=>{
 const f=fixture();let finish!:(value:unknown)=>void,epoch=0;
 f.request.mockImplementation(async(path,options)=>path==='/api/v1/me'?{id:id(1),email:'fixture@example.test',locale:'en',theme:'system',timezone:'UTC',revision:'1'}:options?.method==='POST'?new Promise(resolve=>{finish=resolve}):f.row);
 // Keep the actual client stable across the parent epoch replacement.
 const client={request:f.request};const stableUI=()=>{const captured=epoch;return <CatalogCreateControls client={client as never} origin="https://catalog.test" tenant={id(2)} kind="project" enabled current={()=>epoch===captured} onCreated={f.created}/>};
 const view=render(stableUI());fireEvent.change(screen.getByLabelText('Project name'),{target:{value:'Wedding'}});await enabled('Create');
 fireEvent.change(screen.getByLabelText('Project name'),{target:{value:'Wedding'}});await clickWhenEnabled(screen.getByText('Create'));await waitFor(()=>expect(f.request.mock.calls.some(([,o])=>o?.method==='POST')).toBe(true));
 epoch++;view.rerender(stableUI());await act(async()=>finish(f.row));expect(f.created).not.toHaveBeenCalled();
 expect(f.request.mock.calls.filter(([path,o])=>path.includes('/projects/')&&o?.method!=='POST')).toHaveLength(0);
});

it.each([{name:{unexpected:true}},{revision:'01'},{revision:'9223372036854775808'},{download_allowed:'yes'},{description:[]},{created_at:'yesterday'},{can_edit:undefined},{tenant_id:id(20)}])('rejects malformed project recovery GET %j while preserving accepted identity for GET-only retry',async patch=>{
 const f=fixture();let reads=0;f.request.mockImplementation(async(path,options)=>path==='/api/v1/me'?{id:id(1),email:'fixture@example.test',locale:'en',theme:'system',timezone:'UTC',revision:'1'}:options?.method==='POST'?f.row:reads++===0?{...f.row,...patch}:f.row);
 render(f.ui());fireEvent.change(screen.getByLabelText('Project name'),{target:{value:'Wedding'}});await enabled('Create');
 fireEvent.change(screen.getByLabelText('Project name'),{target:{value:'Wedding'}});await clickWhenEnabled(screen.getByText('Create'));await screen.findByText(en.catalog_create_uncertain);expect(f.created).not.toHaveBeenCalled();
 await enabled('Recover saved creation');await clickWhenEnabled(screen.getByRole('button',{name:'Recover saved creation'}));await waitFor(()=>expect(f.created).toHaveBeenCalledOnce());expect(f.request.mock.calls.filter(([,o])=>o?.method==='POST')).toHaveLength(1);
});


it.each([['project','ru'],['project','bg'],['style','ru'],['style','bg']] as const)('keeps exact %s creation while changing language to %s',async(kind,language)=>{
 const f=fixture(kind);let lost=true;f.request.mockImplementation(async(path,options)=>{if(path==='/api/v1/me')return{id:id(1)};if(options?.method==='POST'&&lost){lost=false;throw new Error('lost reply')}return f.row;});
 const view=render(f.ui());const keyName=kind==='project'?'catalog_project_name':'catalog_style_name';
 fireEvent.change(screen.getByRole('textbox',{name:en[keyName]}),{target:{value:'Wedding'}});await enabled(en.catalog_create);await clickWhenEnabled(screen.getByRole('button',{name:en.catalog_create}));
 await screen.findByText(en.catalog_create_uncertain);const first=f.request.mock.calls.find(([,options])=>options?.method==='POST')!;const body=structuredClone(first[1]!.body),key=first[1]!.idempotencyKey;
 state.locale=language;view.rerender(f.ui());const text=catalogs[language];
 expect(screen.getByRole('alert').textContent).toBe(text.catalog_create_uncertain);expect(screen.getByRole('textbox',{name:text[keyName]}).getAttribute('readonly')).not.toBeNull();
 await clickWhenEnabled(screen.getByRole('button',{name:text.catalog_recover_create}));await waitFor(()=>expect(f.created).toHaveBeenCalledOnce());
 const posts=f.request.mock.calls.filter(([,options])=>options?.method==='POST');expect(posts).toHaveLength(2);expect(posts[1][0]).toBe(first[0]);expect(posts[1][1]?.body).toEqual(body);expect(posts[1][1]?.idempotencyKey).toBe(key);
});
