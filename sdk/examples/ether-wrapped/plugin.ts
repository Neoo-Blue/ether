import { plugin, type Manifest } from '../../typescript/src/index.ts';
import manifest from './manifest.json' with { type: 'json' };
import en from './i18n/en.json' with { type: 'json' };
import fr from './i18n/fr.json' with { type: 'json' };

type History = { titleId: string; type: string; watchedAt: string; completed?: boolean };
type Title = { id: string; title: string; type: string; tmdbId?: number; imdbId?: string; year?: number; genres?: string[]; posterPath?: string; summary?: string };
async function yearData(ctx: any, viewer: number, year: number) {
  if (!Number.isSafeInteger(viewer) || viewer <= 0 || year < 1900 || year > 9999) return { titles: [], plays: 0, genres: [], months: Array(12).fill(0) };
  const viewers = await ctx.ether.viewers.granted();
  if (!viewers.some((v: any) => v.id === viewer)) return { titles: [], plays: 0, genres: [], months: Array(12).fill(0) };
  const history: History[] = await ctx.ether.viewers.history(viewer, 200, 0);
  const counts = new Map<string, number>();
  const months = Array(12).fill(0);
  for (const entry of history) {
    const date = new Date(entry.watchedAt);
    if (!entry.completed || date.getUTCFullYear() !== year) continue;
    let titleId = entry.titleId;
    if (entry.type === 'episode') { const key = Number(titleId); if (Number.isSafeInteger(key) && key >= 4e15 && key < 5e15) titleId = String(2e15 + Math.floor((key - 4e15) / 1e7)); }
    counts.set(titleId, (counts.get(titleId) || 0) + 1);
    months[date.getUTCMonth()]++;
  }
  const titles: Array<Title & {count: number}> = [];
  for (const [id, count] of counts) {
    try {
      const title = await ctx.ether.catalog.get(id) as Title | null;
      if (title) titles.push({ ...title, count });
    } catch { /* A removed title should not hide the rest of the year. */ }
  }
  titles.sort((a,b) => b.count-a.count || a.title.localeCompare(b.title));
  const genreCounts = new Map<string,number>();
  for (const title of titles) for (const genre of title.genres || []) genreCounts.set(genre, (genreCounts.get(genre) || 0) + title.count);
  const genres = [...genreCounts].sort((a,b) => b[1]-a[1] || a[0].localeCompare(b[0])).slice(0,5);
  return { titles, plays: months.reduce((a,b) => a+b,0), genres, months };
}
export const wrappedDefinition = { manifest: manifest as Manifest, strings: { en, fr }, routes: {
  async view(req, ctx) {
    const viewers = await ctx.ether.viewers.granted();
    const viewer = Number(req.query.viewer || viewers[0]?.id || 0);
    const year = Number(req.query.year || new Date().getUTCFullYear());
    const result = await yearData(ctx, viewer, year);
    const data: Record<string,unknown> = { year: String(year), plays: String(result.plays), titleCount: String(result.titles.length), genres: result.genres.map(([g,n]) => `${g} (${n})`).join(', '), titles: result.titles.map(t => ({ ratingKey:t.id, title:t.title, type:t.type, poster:t.posterPath ? `https://image.tmdb.org/t/p/w300${t.posterPath}` : undefined })), series: [{ key:'plays', label:ctx.t('plays'), values:result.months }], times: result.months.map((_,i) => `${year}-${String(i+1).padStart(2,'0')}-01T00:00:00Z`) };
    const page = Math.max(0, Math.min(Math.ceil(viewers.length / 6) - 1, Number(req.query.viewerPage) || 0));
    const links = viewers.slice(page * 6, page * 6 + 6).map((v,i) => { data[`viewer${i}`] = v.name; data[`active${i}`] = v.id === viewer; return { href:`?viewer=${v.id}&viewerPage=${page}`, label:{$:`viewer${i}`}, activeWhen:{$:`active${i}`} }; });
    data.neverActive = false;
    if (page > 0) links.unshift({href:`?viewerPage=${page-1}`, label:'$t:previous' as any, activeWhen:{$:'neverActive'}});
    if ((page + 1) * 6 < viewers.length) links.push({href:`?viewerPage=${page+1}`, label:'$t:next' as any, activeWhen:{$:'neverActive'}});
    return { spec: { specVersion:1, layout:'list', header:[{kind:'page-header',title:'$t:page',description:'$t:description'}], body:[{kind:'filter-bar',controls:{},leading:{kind:'toggle-links',links}}, {kind:'stat-tile',iconKey:'calendar',accent:'violet',label:'$t:year',value:{$:'year'}},{kind:'stat-tile',iconKey:'play',accent:'teal',label:'$t:plays',value:{$:'plays'}},{kind:'stat-tile',iconKey:'film',accent:'blue',label:'$t:titles',value:{$:'titleCount'}},{kind:'heading',level:2,content:'$t:top_titles'},{kind:'widget',widget:'ether.posterGrid',props:{items:{$:'titles'},empty:'$t:empty'}},{kind:'panel',title:'$t:genres',blocks:[{kind:'text',content:{$:'genres'}}]},{kind:'heading',level:2,content:'$t:activity'},{kind:'widget',widget:'ether.liveChart',props:{series:{$:'series'},times:{$:'times'},label:'$t:plays'}}] }, data };
  },
  async catalog(req,ctx) {
    const viewer = Number(req.extra.viewerId || 0), year = Number(req.extra.year || new Date().getUTCFullYear());
    const data = await yearData(ctx, viewer, year);
    return { metas: data.titles.filter(t => (req.type === 'series' ? t.type === 'show' : t.type === req.type) && t.imdbId).slice(req.skip,req.skip+100).map(t => ({id:t.imdbId!,type:req.type,name:t.title,releaseInfo:String(t.year || ''),description:t.summary})) };
  }
}};
export const wrappedPlugin = plugin(wrappedDefinition);
export const handle = wrappedPlugin.handle;
