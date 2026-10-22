import {it,expect,vi} from 'vitest';
import {materializeSemanticShownVersion} from './semantic-shown-version';
const id='11111111-1111-4111-8111-111111111111';
it('uses bodyless target-scoped POST and owns returned version',async()=>{
 const response={asset_version_id:id,asset_id:id},request=vi.fn().mockResolvedValue(response),controller=new AbortController();
 const result=await materializeSemanticShownVersion({request},id,id,controller.signal);
 expect(request).toHaveBeenCalledWith(`/api/v1/tenants/${id}/semantic-targets/${id}/version`,{method:'POST',signal:controller.signal});
 response.asset_version_id='changed';expect(result.asset_version_id).toBe(id);
});
it('rejects invalid scope before sending and refuses malformed server version',async()=>{
 const request=vi.fn().mockResolvedValue({asset_version_id:id,unexpected:true});
 await expect(materializeSemanticShownVersion({request},'invalid',id)).rejects.toThrow('REQUEST_INVALID');expect(request).not.toHaveBeenCalled();
 await expect(materializeSemanticShownVersion({request},id,id)).rejects.toThrow('RESPONSE_INVALID');
});
it('refuses a version without its authorized editor asset',async()=>{
 const request=vi.fn().mockResolvedValue({asset_version_id:id});
 await expect(materializeSemanticShownVersion({request},id,id)).rejects.toThrow('RESPONSE_INVALID');
 request.mockResolvedValue({asset_version_id:id,asset_id:'00000000-0000-0000-0000-000000000000'});
 await expect(materializeSemanticShownVersion({request},id,id)).rejects.toThrow('RESPONSE_INVALID');
});
