// @vitest-environment jsdom
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {act,render,screen,fireEvent,waitFor,cleanup} from '@testing-library/react';
import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import {APIError} from '@photoeditor/api-client';
import en from '../../../locales/en/common.json';
const mocks=vi.hoisted(()=>({api:{draft:vi.fn(),me:vi.fn(),mediaLinks:vi.fn(),capabilities:vi.fn(),collection:vi.fn(),styles:vi.fn(),styleVersions:vi.fn(),saveDraft:vi.fn(),v2Draft:vi.fn(),v2Binding:vi.fn(),v2Document:vi.fn(),v2Layers:vi.fn(),saveV2Draft:vi.fn()},query:{tenant:'11111111-1111-4111-8111-111111111111',asset:'asset',version:'22222222-2222-4222-8222-222222222222',collection:''},auth:{origin:'https://editor-test',client:()=>({})},storage:new Map<string,string>()}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('expo-router',()=>({useLocalSearchParams:()=>mocks.query,useRouter:()=>({push:vi.fn()})}));
vi.mock('./auth-context',()=>({useAuth:()=>mocks.auth}));
vi.mock('./owner-api',()=>({OwnerAPI:class{constructor(){return mocks.api}}}));
vi.mock('./locale-context',()=>({useLocale:()=>({t:(key:keyof typeof en)=>en[key]})}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(k:string)=>mocks.storage.get(k)??null,setItem:async(k:string,v:string)=>{mocks.storage.set(k,v)},removeItem:async(k:string)=>{mocks.storage.delete(k)},getAllKeys:async()=>[...mocks.storage.keys()]}}));
vi.mock('./editor-panels',()=>({EditorPanels:()=>null}));
vi.mock('./mask-canvas',()=>({MaskCanvas:()=>null}));
vi.mock('./compare-canvas',()=>({CompareCanvas:()=>null}));
import Editor from '../app/editor/[version]';
const base={asset_version_id:'22222222-2222-4222-8222-222222222222',source_sha256:'a'.repeat(64),revision:'4',params:{schema_version:1,modules:{strength:1}},mask_refs:[]};
afterEach(cleanup);
beforeEach(()=>{
 vi.resetAllMocks();mocks.storage.clear();mocks.query.asset='asset';mocks.query.version=base.asset_version_id;mocks.query.collection='';
 mocks.api.draft.mockResolvedValue(base);mocks.api.me.mockResolvedValue({id:'33333333-3333-4333-8333-333333333333'});
 mocks.api.mediaLinks.mockResolvedValue({items:[]});mocks.api.capabilities.mockResolvedValue({operations:[]});
});
it('compares a CAS conflict and retries local edits with the fetched revision',async()=>{
 const server={...base,revision:'5',params:{schema_version:1,modules:{strength:.8}}};
 mocks.api.draft.mockResolvedValueOnce(base).mockResolvedValue(server);
 mocks.api.saveDraft.mockRejectedValueOnce(new APIError(409,'REVISION_CONFLICT')).mockImplementationOnce(async(_asset,payload)=>({...payload,revision:'6'}));
 render(<Editor/>);
 const input=screen.getByLabelText('Editor recipe');
 await waitFor(()=>expect((input as HTMLTextAreaElement).value).toContain('strength'));
 const local={schema_version:1,modules:{strength:.4}};
 fireEvent.change(input,{target:{value:JSON.stringify(local)}});
 fireEvent.click(screen.getByRole('button',{name:'Save draft'}));
 await screen.findByText(en.editor_conflict_title);
 expect(mocks.api.saveDraft).toHaveBeenCalledTimes(1);
 await waitFor(()=>expect(screen.getByRole('button',{name:en.editor_conflict_keep_local}).getAttribute('aria-disabled')).not.toBe('true'));
 fireEvent.click(screen.getByRole('button',{name:en.editor_conflict_keep_local}));
 expect((input as HTMLTextAreaElement).value).toBe(JSON.stringify(local));
 fireEvent.click(screen.getByRole('button',{name:'Save draft'}));
 await waitFor(()=>expect(mocks.api.saveDraft).toHaveBeenCalledTimes(2));
 expect(mocks.api.saveDraft.mock.calls[1][1]).toMatchObject({revision:'5',params:local});
 await waitFor(()=>expect(screen.queryByText(en.editor_conflict_title)).toBeNull());
});

it('keeps a newer recipe while a save response is pending',async()=>{
 let finish!:(value:unknown)=>void;
 mocks.api.saveDraft.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve}));
 render(<Editor/>);
 const input=screen.getByLabelText('Editor recipe');
 await waitFor(()=>expect((input as HTMLTextAreaElement).value).toContain('strength'));
 const submitted={schema_version:1,modules:{strength:.4}},newer={schema_version:1,modules:{strength:.2}};
 fireEvent.change(input,{target:{value:JSON.stringify(submitted)}});
 fireEvent.click(screen.getByRole('button',{name:'Save draft'}));
 await waitFor(()=>expect(mocks.api.saveDraft).toHaveBeenCalledTimes(1));
 fireEvent.change(input,{target:{value:JSON.stringify(newer)}});
 await act(async()=>{finish({...base,params:submitted,revision:'5'})});
 expect((input as HTMLTextAreaElement).value).toBe(JSON.stringify(newer));
 await screen.findByText('The submitted revision was saved. Your newer local edits remain unsaved.');
 await waitFor(()=>expect([...mocks.storage.values()].some(raw=>{
  const cached=JSON.parse(raw);return cached.base.revision==='5'&&cached.local.params.modules.strength===.2;
 })).toBe(true));
});

it('ignores a late legacy save acknowledgement after switching photos',async()=>{
 let finish!:(value:unknown)=>void;
 const next={...base,asset_version_id:'44444444-4444-4444-8444-444444444444',source_sha256:'b'.repeat(64),params:{schema_version:1,modules:{strength:.7}}};
 mocks.api.draft.mockImplementation(async asset=>asset==='other'?next:base);
 mocks.api.saveDraft.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve}));
 const view=render(<Editor/>);
 await waitFor(()=>expect((screen.getByLabelText('Editor recipe') as HTMLTextAreaElement).value).toContain('strength'));
 fireEvent.click(screen.getByRole('button',{name:'Save draft'}));
 await waitFor(()=>expect(mocks.api.saveDraft).toHaveBeenCalledTimes(1));
 mocks.query.asset='other';mocks.query.version=next.asset_version_id;
 view.rerender(<Editor/>);
 await waitFor(()=>expect((screen.getByLabelText('Editor recipe') as HTMLTextAreaElement).value).toContain('0.7'));
 await act(async()=>{finish({...base,revision:'5'})});
 expect((screen.getByLabelText('Editor recipe') as HTMLTextAreaElement).value).toContain('0.7');
 expect(screen.queryByText('Draft could not be saved. Check the recipe and reload if its revision changed.')).toBeNull();
});

it('keeps collection styles scoped when an older request finishes late',async()=>{
 let finishOld!:(value:unknown)=>void;
 mocks.query.collection='old';
 mocks.api.collection.mockImplementation(async id=>id==='old'?await new Promise(resolve=>{finishOld=resolve}):{project_id:'new-project'});
 mocks.api.styles.mockImplementation(async project=>({items:[{id:project,name:project}]}));
 const view=render(<Editor/>);
 await waitFor(()=>expect(mocks.api.collection).toHaveBeenCalledWith('old'));
 mocks.query.collection='new';view.rerender(<Editor/>);
 await screen.findByText('new-project');
 await act(async()=>{finishOld({project_id:'old-project'})});
 expect(screen.queryByText('old-project')).toBeNull();
 expect(screen.getByText('new-project')).toBeTruthy();
});

it('keeps the latest style version choice within one collection',async()=>{
 let finishOld!:(value:unknown)=>void;
 mocks.query.collection='current';
 mocks.api.collection.mockResolvedValue({project_id:'project'});
 mocks.api.styles.mockResolvedValue({items:[{id:'first',name:'First style'},{id:'second',name:'Second style'}]});
 mocks.api.styleVersions.mockImplementation(async id=>id==='first'?await new Promise(resolve=>{finishOld=resolve}):{items:[{id:'new-version',version_no:'2',approval_status:'accepted',export_artifact_id:'artifact'}]});
 render(<Editor/>);
 fireEvent.click(await screen.findByText('First style'));
 fireEvent.click(screen.getByText('Second style'));
 await screen.findByText('Create V2 draft · style version 2');
 await act(async()=>{finishOld({items:[{id:'old-version',version_no:'1',approval_status:'accepted',export_artifact_id:'artifact'}]})});
 expect(screen.queryByText('Create V2 draft · style version 1')).toBeNull();
 expect(screen.getByText('Create V2 draft · style version 2')).toBeTruthy();
});

it('restores cached local edits against an original base and compares newer server revision',async()=>{
 const local={schema_version:1,modules:{strength:.3}};
 const key='photo.editor-draft.v1:'+JSON.stringify([mocks.auth.origin,'33333333-3333-4333-8333-333333333333','11111111-1111-4111-8111-111111111111',base.asset_version_id]);
 mocks.storage.set(key,JSON.stringify({schema_version:1,base,local:{params:local,maskRefs:[]},saved_at:Date.now()}));
 mocks.api.draft.mockResolvedValue({...base,revision:'9'});
 render(<Editor/>);
 await screen.findByText(en.editor_conflict_title);
 expect((screen.getByLabelText('Editor recipe') as HTMLTextAreaElement).value).toContain('0.3');
 expect(mocks.api.saveDraft).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole('button',{name:en.editor_conflict_keep_local}));
 mocks.api.saveDraft.mockImplementationOnce(async(_asset,payload)=>({...payload,revision:'10'}));
 fireEvent.click(screen.getByRole('button',{name:'Save draft'}));
 await waitFor(()=>expect(mocks.api.saveDraft).toHaveBeenCalledTimes(1));
 expect(mocks.api.saveDraft.mock.calls[0][1]).toMatchObject({revision:'9',params:local});
});
it('refuses a cached snapshot from a changed source without a server write',async()=>{
 const key='photo.editor-draft.v1:'+JSON.stringify([mocks.auth.origin,'33333333-3333-4333-8333-333333333333','11111111-1111-4111-8111-111111111111',base.asset_version_id]);
 mocks.storage.set(key,JSON.stringify({schema_version:1,base:{...base,source_sha256:'b'.repeat(64)},local:{params:{schema_version:1,modules:{strength:.1}},maskRefs:[]},saved_at:Date.now()}));
 render(<Editor/>);
 await screen.findByText(en.editor_conflict_source_changed);
 expect((screen.getByLabelText('Editor recipe') as HTMLTextAreaElement).value).toContain('"strength": 1');
 expect(screen.queryByText(en.editor_conflict_title)).toBeNull();
 expect(mocks.api.saveDraft).not.toHaveBeenCalled();
});

const layerId=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const layerDigest=(v:unknown)=>bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(v))));
function configureLayers(){
 const binding={asset_version_id:base.asset_version_id,source:{artifact_id:layerId(1),sha256:base.source_sha256},canvas_hash:base.source_sha256};
 const recipe={schema_version:3,source:binding,base_style:{artifact_id:layerId(2),sha256:base.source_sha256},session_corrections:null,learning_version_id:null,overrides:[],layer_stack:null,renderer_version:'v2.2',parameter_delta:{ev:0,log_r:0,log_g:0,log_b:0,saturation_delta:0,retouch_delta:0}};
 const draft={id:layerId(3),revision:'2',document:{artifact_id:layerId(4),sha256:layerDigest(recipe)},data:{id:layerId(3),revision:'2',resource_type:'v2_document_streams',stream_type:'draft',asset_version_id:base.asset_version_id,current_document_id:layerId(5)}};
 mocks.api.v2Draft.mockResolvedValue(draft);mocks.api.v2Binding.mockResolvedValue(binding);mocks.api.v2Document.mockResolvedValue(recipe);
 return {recipe,draft,commit:(layers:unknown)=>{
  const committed={...recipe,layer_stack:{artifact_id:layerId(6),sha256:layerDigest(layers)}};
  mocks.api.v2Document.mockResolvedValue(committed);
  return {...draft,revision:'3',document:{artifact_id:layerId(7),sha256:layerDigest(committed)},data:{...draft.data,revision:'3',current_document_id:layerId(8)}};
 }};
}
async function openLayers(){
 await screen.findByText(/revision 4/);
 await waitFor(()=>expect(screen.getByRole('button',{name:en.layers_load}).getAttribute('aria-disabled')).not.toBe('true'));
 fireEvent.click(screen.getByRole('button',{name:en.layers_load}));
 await screen.findByRole('button',{name:en.layers_save});
}
it('retries the same V2 layer request and key after losing the response',async()=>{
 const fixture=configureLayers();mocks.api.saveV2Draft.mockRejectedValueOnce(new Error('offline')).mockImplementationOnce(async(_asset,_revision,_recipe,layers)=>fixture.commit(layers));
 render(<Editor/>);await openLayers();
 fireEvent.click(screen.getByRole('button',{name:en.layers_save}));await screen.findByText(en.layers_save_retry);
 expect(screen.getByRole('button',{name:en.layers_load}).getAttribute('aria-disabled')).toBe('true');
 await waitFor(()=>expect(screen.getByRole('button',{name:en.layers_retry_save}).hasAttribute('disabled')).toBe(false));
 fireEvent.click(screen.getByRole('button',{name:en.layers_retry_save}));await screen.findByText(en.layers_saved);
 expect(mocks.api.saveV2Draft).toHaveBeenCalledTimes(2);
 expect(mocks.api.saveV2Draft.mock.calls[1].slice(0,5)).toEqual(mocks.api.saveV2Draft.mock.calls[0].slice(0,5));
 expect(screen.getByText(/revision 4/)).toBeTruthy();
});
it('preserves recipe edits made while the V2 layer save is pending',async()=>{
 const fixture=configureLayers();let finish!:(value:unknown)=>void;
 mocks.api.saveV2Draft.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve}));
 render(<Editor/>);await openLayers();fireEvent.click(screen.getByRole('button',{name:en.layers_save}));
 await waitFor(()=>expect(mocks.api.saveV2Draft).toHaveBeenCalledTimes(1));
 const local=JSON.stringify({schema_version:1,modules:{strength:.23}});fireEvent.change(screen.getByLabelText('Editor recipe'),{target:{value:local}});
 await act(async()=>finish(fixture.commit(mocks.api.saveV2Draft.mock.calls[0][3])));
 await screen.findByText(en.layers_saved);expect((screen.getByLabelText('Editor recipe') as HTMLTextAreaElement).value).toBe(local);
});
it('ignores a late V2 save acknowledgement after switching photos',async()=>{
 const fixture=configureLayers();let finish!:(value:unknown)=>void;
 mocks.api.saveV2Draft.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve}));
 const view=render(<Editor/>);await openLayers();fireEvent.click(screen.getByRole('button',{name:en.layers_save}));
 await waitFor(()=>expect(mocks.api.saveV2Draft).toHaveBeenCalledTimes(1));
 const signal=mocks.api.saveV2Draft.mock.calls[0][5] as AbortSignal;
 mocks.query.asset='another';mocks.query.version=layerId(10);mocks.api.draft.mockResolvedValue({...base,asset_version_id:layerId(10)});
 view.rerender(<Editor/>);await waitFor(()=>expect(signal.aborted).toBe(true));
 await act(async()=>finish(fixture.commit(mocks.api.saveV2Draft.mock.calls[0][3])));
 expect(screen.queryByText(en.layers_saved)).toBeNull();expect(screen.queryByRole('button',{name:en.layers_save})).toBeNull();
});
it('requires refresh rather than another write after an acknowledged but unreadable commit',async()=>{
 const fixture=configureLayers();
 mocks.api.saveV2Draft.mockImplementationOnce(async(_asset,_revision,_recipe,layers)=>{
  const saved=fixture.commit(layers);mocks.api.v2Document.mockRejectedValueOnce(new Error('document offline'));return saved;
 });
 render(<Editor/>);await openLayers();fireEvent.click(screen.getByRole('button',{name:en.layers_save}));
 await screen.findByText(en.layers_saved_refresh);
 expect(screen.queryByRole('button',{name:en.layers_retry_save})).toBeNull();
 expect(screen.queryByRole('button',{name:en.layers_save})).toBeNull();
 expect(screen.getByRole('button',{name:en.layers_load}).getAttribute('aria-disabled')).not.toBe('true');
 expect(mocks.api.saveV2Draft).toHaveBeenCalledTimes(1);
});
it('clears the stale V2 baseline after a CAS rejection',async()=>{
 configureLayers();mocks.api.saveV2Draft.mockRejectedValueOnce(new APIError(409,'REVISION_CONFLICT'));
 render(<Editor/>);await openLayers();fireEvent.click(screen.getByRole('button',{name:en.layers_save}));
 await screen.findByText(en.layers_save_reload);
 expect(screen.queryByRole('button',{name:en.layers_save})).toBeNull();
 expect(screen.queryByRole('button',{name:en.layers_retry_save})).toBeNull();
 expect(mocks.api.saveV2Draft).toHaveBeenCalledTimes(1);
});
