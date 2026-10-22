import {it,expect} from 'vitest';
import {readEditorSourcePreview} from './editor-source-preview-link';
const version='11111111-1111-4111-8111-111111111111';
const row=()=>({variant:'preview1600',asset_version_id:version,url:'https://media.example/preview?signature=opaque',expires_at:'2030-01-01T00:00:00Z',width:1600,height:1000,media_type:'image/jpeg'});
it('owns the exact requested preview link',()=>{
 const item=row(),result=readEditorSourcePreview({items:[item]},version,0);item.url='https://other.example';expect(result.url).toBe('https://media.example/preview?signature=opaque');
});
it('rejects foreign, duplicated, missing and malformed rows',()=>{
 for(const items of [[],[row(),row()],[{...row(),asset_version_id:'foreign'}],[{...row(),variant:'preview2560'}],[null],[{...row(),width:1601}],[{...row(),height:0}],[{...row(),media_type:'text/html'}],[{...row(),expires_at:'invalid'}],[{...row(),expires_at:'1970-01-01T00:00:00Z'}]])expect(()=>readEditorSourcePreview({items},version,1)).toThrow('EDITOR_SOURCE_PREVIEW_INVALID');
});
it('rejects unsafe media URLs while preserving local development',()=>{
 for(const url of ['javascript:alert(1)','file:///tmp/image','http://media.example/image','https://user:secret@media.example/image','https://media.example/image#fragment'])expect(()=>readEditorSourcePreview({items:[{...row(),url}]},version,0)).toThrow();
 for(const url of ['http://localhost:9000/image','http://127.0.0.1:9000/image'])expect(readEditorSourcePreview({items:[{...row(),url}]},version,0).url).toBe(url);
});
