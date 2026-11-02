import { plugin, type Manifest, type CandidateFacts } from '../../typescript/src/index.ts';
import manifest from './manifest.json' with { type: 'json' };

type Config = { value?: string[] };
const aliases: Record<string,string> = { english:'en', eng:'en', french:'fr', fre:'fr', fra:'fr', german:'de', ger:'de', deu:'de', spanish:'es', spa:'es', italian:'it', ita:'it', japanese:'ja', jpn:'ja', portuguese:'pt', por:'pt' };
function normalize(s: string): string { const v = s.toLowerCase().trim(); return aliases[v] || v; }
function available(c: CandidateFacts): string[] {
  const langs = (c.languages || []).map(normalize);
  const tokens = (c.name || '').toLowerCase().split(/[^a-z0-9]+/);
  for (const token of tokens) if (aliases[token]) langs.push(aliases[token]); else if (['en','fr','de','es','it','ja','pt','ko','zh','hi'].includes(token)) langs.push(token);
  if (tokens.includes('multi')) langs.push('multi');
  return langs;
}
function matches(c: CandidateFacts, lang: string): boolean {
  const langs = available(c), target = normalize(lang);
  return !!target && (langs.includes(target) || langs.includes('multi'));
}
export const dubPlugin = plugin<Config>({ manifest: manifest as Manifest, routes: {
  rankKey(c, cfg) {
    const langs = Array.isArray(cfg?.value) ? cfg.value : [];
    for (let i = 0; i < langs.length; i++) if (matches(c, langs[i])) return langs.length - i;
    return 0;
  },
  condMatch(c, op, value) {
    if (op === 'equals') return typeof value === 'string' && matches(c, value);
    if (op === 'in') { const values = Array.isArray(value) ? value : typeof value === 'string' ? value.split(',') : []; return values.some(v => typeof v === 'string' && matches(c, v)); }
    return false;
  }
}});
export const handle = dubPlugin.handle;
export const rank_key = dubPlugin.rank_key;
export const cond_match = dubPlugin.cond_match;
