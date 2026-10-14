// @vitest-environment jsdom
import React from 'react';
import {readFileSync} from 'node:fs';
import {it,expect,vi} from 'vitest';
import {render,screen,fireEvent,waitFor,cleanup} from '@testing-library/react';
import {decodeRunOutputs} from '@photoeditor/api-client';
vi.mock('react-native',async()=>import('react-native-web'));
import {SemanticPreview} from './semantic-preview';
const capture=process.env.PHOTO_TEST_V27_OUTPUTS_RESPONSE_FILE;
it.skipIf(!capture)('renders an accepted preview from the real output response',async()=>{
 const fixture=JSON.parse(readFileSync(capture!,'utf8'));
 const page=decodeRunOutputs(fixture.response);
 const runOutputs=vi.fn().mockResolvedValue(page);
 try{
  render(<SemanticPreview client={{runOutputs}} tenant={fixture.tenant} run={fixture.run}/>);
  fireEvent.click(screen.getByText('View preview'));
  await waitFor(()=>expect(Boolean(screen.getByLabelText('Edited photo preview'))).toBe(true));
  expect(runOutputs.mock.calls[0][0]===fixture.tenant&&runOutputs.mock.calls[0][1]===fixture.run).toBe(true);
  expect(screen.queryByRole('alert')===null).toBe(true);
 }finally{cleanup()}
});
