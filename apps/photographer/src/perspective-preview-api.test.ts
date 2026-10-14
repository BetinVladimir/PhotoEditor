import {expect,it,vi} from 'vitest';
import type {ApiClient} from '@photoeditor/api-client';
import {OwnerAPI} from './owner-api';
import type {PerspectiveProposal} from './perspective-proposal';

const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const proposal:PerspectiveProposal={schema_version:1,tenant_id:id(1),project_id:id(2),producer_task_id:id(3),result_id:id(4),result:ref(5),
 base:{asset_version_id:id(6),source:ref(7),canvas_hash:'b'.repeat(64)},working_image:ref(8),changed_mask:ref(9),report:ref(10),
 canvas:{width:100,height:80,orientation:1,space_id:'perspective:canvas',source_sha256:'c'.repeat(64)},status:'review',requires_review:true};

it('does not request a perspective preview after cancellation',async()=>{
 const request=vi.fn(),api=new OwnerAPI({request} as unknown as ApiClient,id(1));
 const controller=new AbortController();controller.abort();
 await expect(api.perspectivePreview(proposal,'preview2560',controller.signal)).rejects.toThrow('PERSPECTIVE_PREVIEW_ABORTED');
 expect(request).not.toHaveBeenCalled();
});
