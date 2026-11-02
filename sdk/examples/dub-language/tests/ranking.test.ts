import test from 'node:test';
import assert from 'node:assert/strict';
import { dubPlugin } from '../plugin.ts';
import recorded from './fixtures/releases.json' with { type: 'json' };

test('ordered languages and condition evaluate audio facts deterministically', () => {
  const french = { releaseId:'1', name:'Release.FRENCH.DUB', languages:['fr'] };
  const english = { releaseId:'2', name:'Release.ENGLISH', languages:['en'] };
  assert.ok(dubPlugin.rank_key(french, {value:['fr','en']}) > dubPlugin.rank_key(english, {value:['fr','en']}));
  assert.equal(dubPlugin.cond_match(french, 'equals', 'fra'), true);
  assert.equal(dubPlugin.cond_match(french, 'in', ['de','fr']), true);
  assert.equal(dubPlugin.cond_match(french, 'in', 'de, fr'), true);
  assert.equal(dubPlugin.cond_match(french, 'equals', 'de'), false);
  assert.equal(dubPlugin.cond_match(french, 'unknown', 'fr'), false);
});

test('recorded markers distinguish unknown dubs and multilingual releases', () => {
  assert.equal(dubPlugin.cond_match(recorded[2], 'equals', 'fr'), true);
  assert.equal(dubPlugin.cond_match(recorded[3], 'equals', 'fr'), false);
});
