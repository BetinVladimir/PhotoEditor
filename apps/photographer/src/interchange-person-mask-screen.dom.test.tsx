// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
const mocks=vi.hoisted(()=>({entries:vi.fn(),candidates:vi.fn(),preview:vi.fn(),media:vi.fn(),decide:vi.fn(),statuses:vi.fn()}));
vi.mock('./owner-api',()=>({OwnerAPI:class {
  importPersonMaskEntries=mocks.entries;importPersonMaskCandidates=mocks.candidates;importPersonMaskPreview=mocks.preview;mediaLinks=mocks.media;decideImportPersonMask=mocks.decide;importPersonMaskStatuses=mocks.statuses;
}}));
vi.mock('./locale-context',()=>({useLocale:()=>({t:(key:string)=>key})}));
import {InterchangePersonMaskScreen} from './interchange-person-mask-screen';
const id=(n:number)=>`${n.toString(16).padStart(8,'0')}-0000-4000-8000-000000000001`;
const png=()=>{const bytes=new Uint8Array(33);bytes.set([137,80,78,71,13,10,26,10,0,0,0,13,73,72,68,82,0,0,4,0,0,0,4,0]);return bytes;};
afterEach(()=>{cleanup();vi.restoreAllMocks();mocks.statuses.mockReset();});
it('requires both photo and verified mask to render before enabling grant',async()=>{
  mocks.statuses.mockResolvedValue([]);
  const entry={entry_id:id(1),target_version_id:id(2),asset_id:id(3),external_id:'frame-1'};
  const decision={action:'grant' as const,entry_id:entry.entry_id,target_version_id:entry.target_version_id,override_artifact_id:id(4),mask_artifact_id:id(5),parsed_jcs:'{}',recipe_artifact_id:id(6),recipe_jcs:'{}',override_jcs:'{}',mask_jcs:JSON.stringify({canvas:{width:1024,height:1024}})};
  mocks.entries.mockResolvedValue({entries:[entry]});mocks.candidates.mockResolvedValue({candidates:[{external_id:entry.external_id,decision}]});mocks.preview.mockResolvedValue(png());
  mocks.media.mockResolvedValue({items:[{asset_version_id:entry.target_version_id,variant:'preview1600',media_type:'image/jpeg',url:'https://example.test/photo.jpg',expires_at:new Date(Date.now()+60000).toISOString(),width:1024,height:1024}]});
  Object.defineProperty(URL,'createObjectURL',{configurable:true,value:vi.fn(()=> 'blob:mask')});Object.defineProperty(URL,'revokeObjectURL',{configurable:true,value:vi.fn()});
  Object.defineProperty(globalThis,'createImageBitmap',{configurable:true,value:vi.fn(async()=>({width:1024,height:1024,close:()=>{}}))});
  render(<InterchangePersonMaskScreen client={{origin:'https://example.test'} as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
  fireEvent.click(await screen.findByRole('button',{name:'frame-1'}));
  fireEvent.click(await screen.findByRole('button',{name:/interchange_mask_candidate/}));
  const grant=await screen.findByRole('button',{name:'interchange_mask_grant'});
  fireEvent.click(grant);expect(mocks.decide).not.toHaveBeenCalled();
  fireEvent.load(screen.getByLabelText('interchange_mask_photo'));fireEvent.load(screen.getByLabelText('interchange_mask_overlay'));
  await waitFor(()=>expect((grant as HTMLButtonElement).disabled).toBe(false));
  mocks.decide.mockResolvedValue({status:'granted',epoch:1});fireEvent.click(grant);
  await waitFor(()=>expect(mocks.decide).toHaveBeenCalledWith(id(8),id(9),decision,expect.any(AbortSignal)));
});
it('lets the editor revoke a saved grant after candidate listing is unavailable',async()=>{
  const saved={entry_id:id(1),target_version_id:id(2),override_artifact_id:id(4),mask_artifact_id:id(5),status:'granted',epoch:3};
  mocks.statuses.mockResolvedValue([saved]);mocks.entries.mockRejectedValue(new Error('not running'));
  mocks.decide.mockResolvedValue({status:'revoked',epoch:4});
  render(<InterchangePersonMaskScreen client={{origin:'https://example.test'} as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
  fireEvent.click(await screen.findByRole('button',{name:'interchange_mask_revoke'}));
  await waitFor(()=>expect(mocks.decide).toHaveBeenCalledWith(id(8),id(9),{action:'revoke',entry_id:saved.entry_id,target_version_id:saved.target_version_id,override_artifact_id:saved.override_artifact_id,mask_artifact_id:saved.mask_artifact_id},expect.any(AbortSignal)));
  await screen.findByText(/interchange_mask_revoked/);
});
