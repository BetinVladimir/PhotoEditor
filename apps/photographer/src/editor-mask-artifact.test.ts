import {it,expect} from 'vitest';
import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import {readEditorMaskArtifact} from './editor-mask-artifact';
function fixture(text=canonicalJSON({protected:false})){const bytes=new TextEncoder().encode(text);return {text,ref:{artifact_id:'11111111-1111-4111-8111-111111111111',role:'prompt_mask_ref',sha256:bytesToHex(sha256(bytes)),bytes:bytes.length,media_type:'application/json',url:'https://media.test/mask?versionId=pinned',expires_at:'2099-01-01T00:00:00Z'}}}
it('reads exact pinned canonical mask bytes',async()=>{const {text,ref}=fixture();expect(await readEditorMaskArtifact(ref,ref.role,new AbortController().signal,async()=>new Response(text))).toEqual({protected:false})});
it('rejects substituted bytes, wrong role and noncanonical JSON',async()=>{
 const {ref}=fixture();await expect(readEditorMaskArtifact(ref,ref.role,new AbortController().signal,async()=>new Response('{"protected":true}'))).rejects.toThrow();
 await expect(readEditorMaskArtifact({...ref,role:'foreign'},ref.role,new AbortController().signal)).rejects.toThrow('EDITOR_MASK_ARTIFACT_INVALID');
 const spaced=fixture('{ "protected": false }');await expect(readEditorMaskArtifact(spaced.ref,spaced.ref.role,new AbortController().signal,async()=>new Response(spaced.text))).rejects.toThrow('EDITOR_MASK_ARTIFACT_INVALID');
});
it('does no IO after cancellation',async()=>{const {ref}=fixture(),controller=new AbortController();controller.abort();let calls=0;await expect(readEditorMaskArtifact(ref,ref.role,controller.signal,async()=>{calls++;return new Response('') })).rejects.toThrow();expect(calls).toBe(0)});
