// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
const mocks=vi.hoisted(()=>({reviews:vi.fn(),saved:vi.fn(),entries:vi.fn(),media:vi.fn(),decide:vi.fn()}));
vi.mock('./owner-api',()=>({OwnerAPI:class {preparedImportReviews=mocks.reviews;savedPreparedImportReviews=mocks.saved;importPersonMaskEntries=mocks.entries;mediaLinks=mocks.media;decidePreparedImportReview=mocks.decide;}}));
vi.mock('./locale-context',()=>({useLocale:()=>({t:(key:string)=>key})}));
import {InterchangePreparedReviewScreen} from './interchange-prepared-review-screen';
const id=(n:number)=>`${n.toString(16).padStart(8,'0')}-0000-4000-8000-000000000001`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const candidate={id:'f'.repeat(64),decision:'pending' as const,bbox:{x:10,y:20,width:5,height:6},score:0.9};
const prepared={entry_id:id(1),target_version_id:id(2),kind:'defects' as const,proposal:ref(3),base:ref(4),document:{version:'defect_candidates_v1',image:ref(4),label_map:ref(5),canvas:{width:100,height:100},candidates:[candidate]},decision_epoch:0};
afterEach(()=>{cleanup();vi.restoreAllMocks();for(const mock of Object.values(mocks))mock.mockReset();});
it('requires the displayed photo and explicit candidate choice before grant',async()=>{
 mocks.reviews.mockResolvedValue([prepared]);mocks.saved.mockResolvedValue([]);mocks.entries.mockResolvedValue({entries:[{entry_id:id(1),target_version_id:id(2),asset_id:id(6),external_id:'frame-1'}]});
 mocks.media.mockResolvedValue({items:[{asset_version_id:id(2),variant:'preview1600',media_type:'image/jpeg',url:'https://example.test/photo.jpg',expires_at:new Date(Date.now()+60000).toISOString(),width:100,height:100}]});
 mocks.decide.mockResolvedValue({status:'granted',epoch:1});
 render(<InterchangePreparedReviewScreen client={{origin:'https://example.test'} as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
 fireEvent.click(await screen.findByRole('button',{name:/interchange_prepared_defects/}));
 const save=await screen.findByRole('button',{name:'interchange_prepared_save'});
 fireEvent.click(save);expect(mocks.decide).not.toHaveBeenCalled();
 fireEvent.load(await screen.findByLabelText('interchange_prepared_photo'));
 fireEvent.click(screen.getByRole('checkbox',{name:'interchange_prepared_accept'}));
 await waitFor(()=>expect((save as HTMLButtonElement).disabled).toBe(false));
 fireEvent.click(save);
 await waitFor(()=>expect(mocks.decide).toHaveBeenCalledWith(id(8),id(9),expect.objectContaining({action:'grant',kind:'defects',accepted_candidate_ids:[candidate.id],expected_epoch:0}),expect.any(String),expect.any(AbortSignal)));
});
it('allows saved decision revoke after the live proposal catalogue stops',async()=>{
 mocks.reviews.mockRejectedValue(new Error('run stopped'));
 mocks.entries.mockResolvedValue({entries:[]});
 mocks.saved.mockResolvedValue([{entry_id:id(1),target_version_id:id(2),kind:'defects',proposal:ref(3),base:ref(4),label_map:ref(5),decision:'accept',accepted_candidate_ids:[candidate.id],rejected_candidate_ids:[],status:'granted',epoch:4}]);
 mocks.decide.mockResolvedValue({status:'revoked',epoch:5});
 render(<InterchangePreparedReviewScreen client={{origin:'https://example.test'} as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
 fireEvent.click(await screen.findByRole('button',{name:'interchange_prepared_revoke'}));
 await waitFor(()=>expect(mocks.decide).toHaveBeenCalledWith(id(8),id(9),expect.objectContaining({action:'revoke',proposal:ref(3),expected_epoch:4}),expect.any(String),expect.any(AbortSignal)));
});
