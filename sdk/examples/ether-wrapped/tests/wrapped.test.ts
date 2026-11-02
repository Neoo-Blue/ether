import test from 'node:test';
import assert from 'node:assert/strict';
import { createTestHarness } from '../../../typescript/src/index.ts';
import { wrappedDefinition } from '../plugin.ts';

test('empty page and list remain safe without a viewer grant', async () => {
  const harness = createTestHarness(wrappedDefinition);
  const page = await harness.view('year');
  assert.equal(page.status, 200);
  assert.equal(page.body.data.plays, '0');
  const list = await harness.catalog('movie', 'year');
  assert.equal(list.status, 200);
  assert.deepEqual(list.body.metas, []);
});
