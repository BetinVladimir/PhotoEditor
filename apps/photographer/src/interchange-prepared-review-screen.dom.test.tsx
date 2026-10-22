// @vitest-environment jsdom
import {clickWhenEnabled} from './test/ui-readiness';
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
vi.mock('react-native',async()=>{const native=await import('react-native-web'),React=await import('react');return {...native,Image:({source,accessibilityLabel,onLoad,onError}:any)=>React.createElement('img',{src:source?.uri,'aria-label':accessibilityLabel,onLoad,onError})};});
const lifecycle=vi.hoisted(()=>({active:true,generation:0}));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>lifecycle}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{}}));
const journal=vi.hoisted(()=>({read:vi.fn(async()=>null),write:vi.fn(async()=>{}),clear:vi.fn(async()=>{})}));
vi.mock('./prepared-review-attempt-store',()=>({PreparedReviewAttemptStore:class {read=journal.read;write=journal.write;clear=journal.clear;}}));
const mocks=vi.hoisted(()=>({reviews:vi.fn(),saved:vi.fn(),entries:vi.fn(),media:vi.fn(),decide:vi.fn()}));
vi.mock('./owner-api',()=>({OwnerAPI:class {me=async()=>({id:'00000001-0000-4000-8000-000000000001'});preparedImportReviewPage=mocks.reviews;savedPreparedImportReviewPage=mocks.saved;importPersonMaskEntries=mocks.entries;mediaLinks=mocks.media;decidePreparedImportReview=mocks.decide;}}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:string)=>key})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
import {InterchangePreparedReviewScreen} from './interchange-prepared-review-screen';
const id=(n:number)=>`${n.toString(16).padStart(8,'0')}-0000-4000-8000-000000000001`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const candidate={id:'f'.repeat(64),decision:'pending' as const,bbox:{x:10,y:20,width:5,height:6},score:0.9};
const prepared={entry_id:id(1),target_version_id:id(2),kind:'defects' as const,proposal:ref(3),base:ref(4),document:{version:'defect_candidates_v1',image:ref(4),label_map:ref(5),canvas:{width:100,height:100},candidates:[candidate]},decision_epoch:0};
afterEach(()=>{cleanup();lifecycle.active=true;lifecycle.generation=0;vi.restoreAllMocks();journal.read.mockReset().mockResolvedValue(null);journal.write.mockReset().mockResolvedValue();journal.clear.mockReset().mockResolvedValue();for(const mock of Object.values(mocks))mock.mockReset();});
it('requires the displayed photo and explicit candidate choice before grant',async()=>{
 mocks.reviews.mockResolvedValue({items:[prepared]});mocks.saved.mockResolvedValue({items:[]});mocks.entries.mockResolvedValue({entries:[{entry_id:id(1),target_version_id:id(2),asset_id:id(6),external_id:'frame-1'}]});
 mocks.media.mockResolvedValue({items:[{asset_version_id:id(2),variant:'preview1600',media_type:'image/jpeg',url:'https://example.test/photo.jpg',expires_at:new Date(Date.now()+60000).toISOString(),width:100,height:100}]});
 mocks.decide.mockResolvedValue({status:'granted',epoch:1});
 render(<InterchangePreparedReviewScreen client={{origin:'https://example.test'} as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:/interchange_prepared_defects/}));
 const save=await screen.findByRole('button',{name:'interchange_prepared_save'});
 fireEvent.click(save);expect(mocks.decide).not.toHaveBeenCalled();
 fireEvent.load(await screen.findByLabelText('interchange_prepared_photo'));
 fireEvent.click(screen.getByRole('checkbox',{name:'interchange_prepared_accept'}));
 await waitFor(()=>expect((save as HTMLButtonElement).disabled).toBe(false));
 const oldPhoto=screen.getByLabelText('interchange_prepared_photo');
 fireEvent.click(screen.getByRole('button',{name:'interchange_prepared_refresh_preview'}));
 await waitFor(()=>expect(screen.getByLabelText('interchange_prepared_photo')).not.toBe(oldPhoto));
 fireEvent.load(oldPhoto);expect((screen.getByRole('button',{name:'interchange_prepared_save'}) as HTMLButtonElement).disabled).toBe(true);
 expect(screen.getByRole('checkbox',{name:'interchange_prepared_accept'}).getAttribute('aria-checked')).toBe('true');
 expect(mocks.decide).not.toHaveBeenCalled();fireEvent.load(screen.getByLabelText('interchange_prepared_photo'));
 await waitFor(()=>expect((screen.getByRole('button',{name:'interchange_prepared_save'}) as HTMLButtonElement).disabled).toBe(false));
 fireEvent.click(screen.getByRole('button',{name:'interchange_prepared_save'}));
 await waitFor(()=>expect(mocks.decide).toHaveBeenCalledWith(id(8),id(9),expect.objectContaining({action:'grant',kind:'defects',accepted_candidate_ids:[candidate.id],expected_epoch:0}),expect.any(String),expect.any(AbortSignal)));
});
it('allows saved decision revoke after the live proposal catalogue stops',async()=>{
 mocks.reviews.mockRejectedValue(new Error('run stopped'));
 mocks.entries.mockResolvedValue({entries:[]});
 mocks.saved.mockResolvedValue({items:[{entry_id:id(1),target_version_id:id(2),kind:'defects',proposal:ref(3),base:ref(4),label_map:ref(5),decision:'accept',accepted_candidate_ids:[candidate.id],rejected_candidate_ids:[],status:'granted',epoch:4}]});
 mocks.decide.mockResolvedValue({status:'revoked',epoch:5});
 render(<InterchangePreparedReviewScreen client={{origin:'https://example.test'} as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'interchange_prepared_revoke'}));
 await waitFor(()=>expect(mocks.decide).toHaveBeenCalledWith(id(8),id(9),expect.objectContaining({action:'revoke',proposal:ref(3),expected_epoch:4}),expect.any(String),expect.any(AbortSignal)));
});
it('shows dust on a separate RAW sensor map rather than over the processed photo',async()=>{
 const dust={...prepared,kind:'dust' as const,document:{algorithm:'sensor_dust_log_v1',camera_id:`raw-${'f'.repeat(64)}`,sensor_width:420,sensor_height:320,raw_crop:{x:10,y:10,width:400,height:300},frames:[ref(4),ref(5),ref(6),ref(7),ref(8)],candidates:[{id:'e'.repeat(64),decision:'pending' as const,sensor_x:20,sensor_y:30,radius_sensor_px:2,observed_frames:5,eligible_frames:5,recurrence:1}],warnings:[]}};
 mocks.reviews.mockResolvedValue({items:[dust]});mocks.saved.mockResolvedValue({items:[]});mocks.entries.mockResolvedValue({entries:[{entry_id:id(1),target_version_id:id(2),asset_id:id(6),external_id:'frame-1'}]});
 mocks.media.mockResolvedValue({items:[{asset_version_id:id(2),variant:'preview1600',media_type:'image/jpeg',url:'https://example.test/photo.jpg',expires_at:new Date(Date.now()+60000).toISOString(),width:100,height:100}]});
 render(<InterchangePreparedReviewScreen client={{origin:'https://example.test'} as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:/interchange_prepared_dust/}));
 expect(await screen.findByText('interchange_prepared_sensor_map_note')).toBeTruthy();
 expect(screen.getByLabelText('interchange_prepared_sensor_map')).toBeTruthy();
 expect(screen.getByLabelText('interchange_prepared_photo')).toBeTruthy();
});

it('navigates prepared and saved catalogues by bounded pages',async()=>{
 const next={...prepared,entry_id:id(10),target_version_id:id(11),proposal:ref(12),base:ref(13),document:{...prepared.document,image:ref(13)}};
 mocks.reviews.mockImplementation(async (_project:string,_workflow:string,after:string)=>after?{items:[next]}:{items:[prepared],next_after:'proposal-cursor'});
 mocks.saved.mockImplementation(async (_project:string,_workflow:string,after:string)=>after?{items:[{entry_id:id(10),target_version_id:id(11),kind:'dust',proposal:ref(12),base:ref(13),label_map:null,decision:'skip',accepted_candidate_ids:[],rejected_candidate_ids:[],status:'granted',epoch:2}]}:{items:[],next_after:'saved-cursor'});
 mocks.entries.mockResolvedValue({entries:[{entry_id:id(1),target_version_id:id(2),asset_id:id(6),external_id:'frame-1'},{entry_id:id(10),target_version_id:id(11),asset_id:id(14),external_id:'frame-2'}]});
 render(<InterchangePreparedReviewScreen client={{origin:'https://example.test'} as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
 const proposalNext=await screen.findByRole('button',{name:'interchange_prepared_next'});
 await waitFor(()=>expect((proposalNext as HTMLButtonElement).disabled).toBe(false));
 fireEvent.click(proposalNext);
 await waitFor(()=>expect(mocks.reviews).toHaveBeenCalledWith(id(8),id(9),'proposal-cursor',expect.any(AbortSignal)));
 const savedNext=await screen.findByRole('button',{name:'interchange_prepared_saved_next'});
 await waitFor(()=>expect((savedNext as HTMLButtonElement).disabled).toBe(false));
 fireEvent.click(savedNext);
 await waitFor(()=>expect(mocks.saved).toHaveBeenCalledWith(id(8),id(9),'saved-cursor',expect.any(AbortSignal)));
 const previous=screen.getByRole('button',{name:'interchange_prepared_previous'});
 await waitFor(()=>expect((previous as HTMLButtonElement).disabled).toBe(false));
 fireEvent.click(previous);
 await waitFor(()=>expect(mocks.reviews).toHaveBeenLastCalledWith(id(8),id(9),'',expect.any(AbortSignal)));
});

it('aborts old account review reads when client changes on the same origin',async()=>{
 mocks.reviews.mockImplementationOnce(()=>new Promise(()=>{})).mockResolvedValue({items:[]});mocks.entries.mockResolvedValue({entries:[]});mocks.saved.mockResolvedValue({items:[]});
 const first={origin:'https://example.test'},next={origin:'https://example.test'};
 const view=render(<InterchangePreparedReviewScreen client={first as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
 await waitFor(()=>expect(mocks.reviews).toHaveBeenCalledTimes(1));const signal=mocks.reviews.mock.calls[0][3] as AbortSignal;
 view.rerender(<InterchangePreparedReviewScreen client={next as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
 expect(signal.aborted).toBe(true);await waitFor(()=>expect(mocks.reviews).toHaveBeenCalledTimes(2));expect(mocks.decide).not.toHaveBeenCalled();
});

it('does not discard an in-flight decision when another proposal is selected',async()=>{
 const other={...prepared,entry_id:id(10),target_version_id:id(11),proposal:ref(12)};
 mocks.reviews.mockResolvedValue({items:[prepared,other]});mocks.saved.mockResolvedValue({items:[]});
 mocks.entries.mockResolvedValue({entries:[{entry_id:id(1),target_version_id:id(2),asset_id:id(6),external_id:'one'},{entry_id:id(10),target_version_id:id(11),asset_id:id(14),external_id:'two'}]});
 mocks.media.mockResolvedValue({items:[{asset_version_id:id(2),variant:'preview1600',media_type:'image/jpeg',url:'https://example.test/photo.jpg',expires_at:new Date(Date.now()+60000).toISOString(),width:100,height:100}]});
 mocks.decide.mockImplementation(()=>new Promise(()=>{}));
 render(<InterchangePreparedReviewScreen client={{origin:'https://example.test'} as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
 const rows=await screen.findAllByRole('button',{name:/interchange_prepared_defects/});fireEvent.click(rows[0]);
 fireEvent.load(await screen.findByLabelText('interchange_prepared_photo'));
 fireEvent.click(screen.getByRole('checkbox',{name:'interchange_prepared_accept'}));
 fireEvent.click(screen.getByRole('button',{name:'interchange_prepared_save'}));
 await waitFor(()=>expect(mocks.decide).toHaveBeenCalledTimes(1));
 const signal=mocks.decide.mock.calls[0][4] as AbortSignal;
 fireEvent.click(rows[1]);expect(signal.aborted).toBe(false);
 expect((rows[1] as HTMLButtonElement).disabled).toBe(true);
 expect(mocks.media).toHaveBeenCalledTimes(1);
});

it('preserves the exact saved revoke attempt after an ambiguous network failure',async()=>{
 mocks.reviews.mockResolvedValue({items:[]});mocks.entries.mockResolvedValue({entries:[]});
 mocks.saved.mockResolvedValue({items:[{entry_id:id(1),target_version_id:id(2),kind:'defects',proposal:ref(3),base:ref(4),label_map:ref(5),decision:'accept',accepted_candidate_ids:[candidate.id],rejected_candidate_ids:[],status:'granted',epoch:4}]});
 mocks.decide.mockRejectedValue(new Error('network'));
 render(<InterchangePreparedReviewScreen client={{origin:'https://example.test'} as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'interchange_prepared_revoke'}));
 await screen.findByRole('alert');
 expect((screen.getByRole('button',{name:'interchange_refresh'}) as HTMLButtonElement).disabled).toBe(true);
 const first=mocks.decide.mock.calls[0];fireEvent.click(screen.getByRole('button',{name:'interchange_prepared_revoke'}));
 await waitFor(()=>expect(mocks.decide).toHaveBeenCalledTimes(2));
 expect(mocks.decide.mock.calls[1][2]).toEqual(first[2]);expect(mocks.decide.mock.calls[1][3]).toBe(first[3]);
});

it('refuses a new decision when the displayed media expires before the click',async()=>{
 const now=Date.now();
 mocks.reviews.mockResolvedValue({items:[prepared]});mocks.saved.mockResolvedValue({items:[]});mocks.entries.mockResolvedValue({entries:[{entry_id:id(1),target_version_id:id(2),asset_id:id(6),external_id:'one'}]});
 mocks.media.mockResolvedValue({items:[{asset_version_id:id(2),variant:'preview1600',media_type:'image/jpeg',url:'https://example.test/photo.jpg',expires_at:new Date(now+60000).toISOString(),width:100,height:100}]});
 render(<InterchangePreparedReviewScreen client={{origin:'https://example.test'} as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:/interchange_prepared_defects/}));fireEvent.load(await screen.findByLabelText('interchange_prepared_photo'));
 fireEvent.click(screen.getByRole('checkbox',{name:'interchange_prepared_accept'}));
 const save=screen.getByRole('button',{name:'interchange_prepared_save'});await waitFor(()=>expect((save as HTMLButtonElement).disabled).toBe(false));
 // Freeze only at the decision boundary. Freezing Date.now while Testing Library
 // and Paper are waiting for readiness can prevent their elapsed-time progress.
 const clock=vi.spyOn(Date,'now').mockReturnValue(now+60001);
 try{fireEvent.click(save);expect(mocks.decide).not.toHaveBeenCalled();}finally{clock.mockRestore();}
});

it('requires the second proposal photo to load after changing selection',async()=>{
 const other={...prepared,entry_id:id(10),target_version_id:id(11),proposal:ref(12)};
 mocks.reviews.mockResolvedValue({items:[prepared,other]});mocks.saved.mockResolvedValue({items:[]});
 mocks.entries.mockResolvedValue({entries:[{entry_id:id(1),target_version_id:id(2),asset_id:id(6),external_id:'one'},{entry_id:id(10),target_version_id:id(11),asset_id:id(14),external_id:'two'}]});
 mocks.media.mockImplementation(async (_asset:string,version:string)=>({items:[{asset_version_id:version,variant:'preview1600',media_type:'image/jpeg',url:`https://example.test/${version}.jpg`,expires_at:new Date(Date.now()+60000).toISOString(),width:100,height:100}]}));
 render(<InterchangePreparedReviewScreen client={{origin:'https://example.test'} as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
 const rows=await screen.findAllByRole('button',{name:/interchange_prepared_defects/});fireEvent.click(rows[0]);
 const oldImage=await screen.findByLabelText('interchange_prepared_photo');fireEvent.load(oldImage);
 fireEvent.click(rows[1]);await waitFor(()=>expect(mocks.media).toHaveBeenCalledTimes(2));
 fireEvent.load(oldImage);
 expect((screen.getByRole('button',{name:'interchange_prepared_skip'}) as HTMLButtonElement).disabled).toBe(true);
 fireEvent.load(screen.getByLabelText('interchange_prepared_photo'));
 await waitFor(()=>expect((screen.getByRole('button',{name:'interchange_prepared_skip'}) as HTMLButtonElement).disabled).toBe(false));
});

it('hides photo authority in background and requires fresh media on foreground',async()=>{
 mocks.reviews.mockResolvedValue({items:[prepared]});mocks.saved.mockResolvedValue({items:[]});mocks.entries.mockResolvedValue({entries:[{entry_id:id(1),target_version_id:id(2),asset_id:id(6),external_id:'one'}]});
 mocks.media.mockResolvedValue({items:[{asset_version_id:id(2),variant:'preview1600',media_type:'image/jpeg',url:'https://example.test/photo.jpg',expires_at:new Date(Date.now()+60000).toISOString(),width:100,height:100}]});
 const client={origin:'https://example.test'};
 const view=render(<InterchangePreparedReviewScreen client={client as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:/interchange_prepared_defects/}));fireEvent.load(await screen.findByLabelText('interchange_prepared_photo'));
 const oldPhoto=screen.getByLabelText('interchange_prepared_photo');
 lifecycle.active=false;view.rerender(<InterchangePreparedReviewScreen client={client as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
 expect(screen.queryByLabelText('interchange_prepared_photo')).toBeNull();expect(screen.queryByRole('button',{name:'interchange_prepared_skip'})).toBeNull();expect(mocks.decide).not.toHaveBeenCalled();
 lifecycle.active=true;lifecycle.generation++;view.rerender(<InterchangePreparedReviewScreen client={client as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:/interchange_prepared_defects/}));
 await waitFor(()=>expect(mocks.media).toHaveBeenCalledTimes(2));expect((screen.getByRole('button',{name:'interchange_prepared_skip'}) as HTMLButtonElement).disabled).toBe(true);
 fireEvent.load(oldPhoto);fireEvent.error(oldPhoto);expect(screen.getByLabelText('interchange_prepared_photo')).not.toBe(oldPhoto);expect((screen.getByRole('button',{name:'interchange_prepared_skip'}) as HTMLButtonElement).disabled).toBe(true);
});

it('restores a saved decision and writes it before exact replay without a live catalogue',async()=>{
 const body={action:'revoke' as const,kind:'dust' as const,entry_id:id(1),target_version_id:id(2),proposal:ref(3),base:ref(4),expected_epoch:3};
 journal.read.mockResolvedValueOnce({version:1,key:id(15),body} as never);
 mocks.reviews.mockRejectedValue(new Error('stopped'));mocks.entries.mockResolvedValue({entries:[]});mocks.saved.mockResolvedValue({items:[]});mocks.decide.mockResolvedValue({workflow_id:id(9),entry_id:id(1),kind:'dust',target_version_id:id(2),proposal_id:id(3),status:'revoked',epoch:4});
 render(<InterchangePreparedReviewScreen client={{origin:'https://example.test'} as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'interchange_prepared_resume'}));
 await waitFor(()=>expect(mocks.decide).toHaveBeenCalledWith(id(8),id(9),body,id(15),expect.any(AbortSignal)));
 expect(journal.write.mock.invocationCallOrder[0]).toBeLessThan(mocks.decide.mock.invocationCallOrder[0]);
 await waitFor(()=>expect(journal.clear).toHaveBeenCalledWith(id(15),expect.objectContaining({epoch:4})));
});

it('retries failed journal restoration without discarding the saved operation',async()=>{
 journal.read.mockRejectedValueOnce(new Error('storage unavailable')).mockResolvedValueOnce({version:1,key:id(15),body:{action:'revoke',kind:'dust',entry_id:id(1),target_version_id:id(2),proposal:ref(3),base:ref(4),expected_epoch:3}} as never);
 mocks.reviews.mockResolvedValue({items:[]});mocks.entries.mockResolvedValue({entries:[]});mocks.saved.mockResolvedValue({items:[]});
 render(<InterchangePreparedReviewScreen client={{origin:'https://example.test'} as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'interchange_prepared_reload'}));
 await screen.findByRole('button',{name:'interchange_prepared_resume'});expect(mocks.decide).not.toHaveBeenCalled();expect(journal.clear).not.toHaveBeenCalled();
});
it('does not send a restored operation after unmount during durable write',async()=>{
 journal.read.mockResolvedValueOnce({version:1,key:id(15),body:{action:'revoke',kind:'dust',entry_id:id(1),target_version_id:id(2),proposal:ref(3),base:ref(4),expected_epoch:3}} as never);
 let release!:()=>void;journal.write.mockImplementationOnce(()=>new Promise<void>(resolve=>{release=resolve;}));
 mocks.reviews.mockResolvedValue({items:[]});mocks.entries.mockResolvedValue({entries:[]});mocks.saved.mockResolvedValue({items:[]});
 const view=render(<InterchangePreparedReviewScreen client={{origin:'https://example.test'} as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'interchange_prepared_resume'}));await waitFor(()=>expect(journal.write).toHaveBeenCalled());
 view.unmount();release();await new Promise(resolve=>setTimeout(resolve,0));expect(mocks.decide).not.toHaveBeenCalled();expect(journal.clear).not.toHaveBeenCalled();
});
it('offers explicit readonly reload after catalogue failure without sending a decision',async()=>{
 mocks.reviews.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({items:[]});mocks.saved.mockResolvedValue({items:[]});mocks.entries.mockResolvedValue({entries:[]});
 render(<InterchangePreparedReviewScreen client={{origin:'https://example.test'} as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'interchange_prepared_reload'}));
 await screen.findByText('interchange_prepared_empty');expect(mocks.decide).not.toHaveBeenCalled();expect(journal.write).not.toHaveBeenCalled();
});

it('cancels an uncertain saved revoke and retries its exact durable key without accepting the late old receipt',async()=>{
 const row={entry_id:id(1),target_version_id:id(2),kind:'defects',proposal:ref(3),base:ref(4),label_map:ref(5),decision:'accept',accepted_candidate_ids:[candidate.id],rejected_candidate_ids:[],status:'granted',epoch:4};
 mocks.reviews.mockResolvedValue({items:[]});mocks.entries.mockResolvedValue({entries:[]});mocks.saved.mockResolvedValue({items:[row]});
 let finish!:(value:unknown)=>void;mocks.decide.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve;})).mockResolvedValueOnce({status:'revoked',epoch:5});
 render(<InterchangePreparedReviewScreen client={{origin:'https://example.test'} as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'interchange_prepared_revoke'}));await waitFor(()=>expect(mocks.decide).toHaveBeenCalledOnce());const original=mocks.decide.mock.calls[0];
 fireEvent.click(screen.getByRole('button',{name:'read_cancel'}));await act(async()=>finish({status:'revoked',epoch:5}));expect(journal.clear).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole('button',{name:'interchange_prepared_resume'}));await waitFor(()=>expect(mocks.decide).toHaveBeenCalledTimes(2));expect(mocks.decide.mock.calls[1][2]).toEqual(original[2]);expect(mocks.decide.mock.calls[1][3]).toBe(original[3]);
});
it('cancels initial catalogue reads and retries them without submitting a decision',async()=>{
 let finish!:(value:unknown)=>void;mocks.reviews.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve;})).mockResolvedValueOnce({items:[]});mocks.saved.mockResolvedValue({items:[]});mocks.entries.mockResolvedValue({entries:[]});
 render(<InterchangePreparedReviewScreen client={{origin:'https://example.test'} as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);await waitFor(()=>expect(mocks.reviews).toHaveBeenCalledOnce());
 fireEvent.click(screen.getByRole('button',{name:'read_cancel'}));await act(async()=>finish({items:[prepared]}));expect(screen.queryByRole('button',{name:/interchange_prepared_defects/})).toBeNull();
 fireEvent.click(screen.getByRole('button',{name:'interchange_prepared_reload'}));await screen.findByText('interchange_prepared_empty');expect(mocks.decide).not.toHaveBeenCalled();
});
