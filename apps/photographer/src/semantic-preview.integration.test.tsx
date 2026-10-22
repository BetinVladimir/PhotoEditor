// @vitest-environment jsdom
import React from 'react';
import {readFileSync} from 'node:fs';
import {it,expect,vi} from 'vitest';
import {render,screen,waitFor,cleanup} from '@testing-library/react';
import {decodeSemanticAppliedPreview} from '@photoeditor/api-client';
vi.mock('react-native',async()=>import('react-native-web'));
import {SemanticPreview} from './semantic-preview';
import {clickWhenEnabled} from './test/ui-readiness';
const capture=process.env.PHOTO_TEST_V27_APPLIED_PREVIEW_RESPONSE_FILE;
it.skipIf(!capture)('renders an accepted preview from the real output response',async()=>{
 const fixture=JSON.parse(readFileSync(capture!,'utf8'));
 const page=decodeSemanticAppliedPreview(fixture.response,fixture.tenant,fixture.target);
 const semanticAppliedPreview=vi.fn().mockResolvedValue(page);
 try{
  render(<SemanticPreview client={{semanticAppliedPreview}} tenant={fixture.tenant} target={fixture.target} run={fixture.run}/>);
  await clickWhenEnabled(screen.getByRole('button',{name:'View preview'}));
  await waitFor(()=>expect(Boolean(screen.getByLabelText('Edited photo preview'))).toBe(true));
  expect(semanticAppliedPreview.mock.calls[0][0]===fixture.tenant&&semanticAppliedPreview.mock.calls[0][1]===fixture.target).toBe(true);
  expect(screen.queryByRole('alert')===null).toBe(true);
 }finally{cleanup()}
});
