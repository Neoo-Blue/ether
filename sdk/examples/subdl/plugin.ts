import { plugin, PluginRouteError } from '../../typescript/src/router.ts';
import { unzipSync, strFromU8 } from './vendor/fflate.js';
import manifest from './manifest.json';
import schema from './settings.schema.json';

const MAX = 2 * 1024 * 1024;
const fail = (message: string): never => { throw new PluginRouteError(502, 'subdl_error', message); };
function bytes64(s: string): Uint8Array {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';
  if (!/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(s) || s.length > 4 * Math.ceil(MAX / 3)) return fail('Invalid subtitle archive.');
  const out: number[] = []; let bits = 0, value = 0;
  for (const c of s.replace(/=+$/, '')) { value = (value << 6) | alphabet.indexOf(c); bits += 6; if (bits >= 8) { bits -= 8; out.push((value >> bits) & 255); } }
  return new Uint8Array(out);
}
function archive(data: string): string {
  const bytes = bytes64(data);
  // Validate the central directory and its declared sizes before inflating.
  let end = -1;
  for (let i = bytes.length - 22; i >= Math.max(0, bytes.length - 65557); i--) if (bytes[i]===80 && bytes[i+1]===75 && bytes[i+2]===5 && bytes[i+3]===6) { end=i; break; }
  if (end < 0) return fail('Invalid subtitle archive.');
  const view = new DataView(bytes.buffer); const count=view.getUint16(end+10,true); let offset=view.getUint32(end+16,true); let total=0;
  if (!count || count > 100 || offset >= end || view.getUint16(end+8,true)!==count || view.getUint16(end+4,true)!==0 || view.getUint16(end+6,true)!==0 || end+22+view.getUint16(end+20,true)!==bytes.length) return fail('Invalid subtitle archive.');
  for (let i=0;i<count;i++) {
    if (offset+46>end || view.getUint32(offset,true)!==0x02014b50) return fail('Invalid subtitle archive.');
    const size=view.getUint32(offset+24,true); total+=size;
    if (total>MAX || (view.getUint16(offset+8,true)&1)) return fail('Subtitle archive is too large or encrypted.');
    offset+=46+view.getUint16(offset+28,true)+view.getUint16(offset+30,true)+view.getUint16(offset+32,true);
  }
  if (offset>end) return fail('Invalid subtitle archive.');
  try {
    const files=unzipSync(bytes); const names=Object.keys(files).filter(n=>/\.srt$/i.test(n) && !n.startsWith('__MACOSX/')).sort();
    if (names.length!==1 || files[names[0]].length>MAX) return fail('The archive must contain one SRT subtitle.');
    return strFromU8(files[names[0]]);
  } catch { return fail('Could not read the subtitle archive.'); }
}
const settingsManifest = {...manifest, ether:{...manifest.ether, settings:schema}} as any;
export const subdl = plugin<{apiKey:string}>({manifest:settingsManifest, routes:{
  subtitles(req, ctx) {
    const id = req.id.split(':');
    const apiKey = ctx.ether.secret.get('apiKey') as string;
    if (!/^tt\d+$/.test(id[0]) || !apiKey) return {subtitles:[]};
    let url='https://api.subdl.com/api/v1/subtitles?api_key='+encodeURIComponent(apiKey)+'&imdb_id='+encodeURIComponent(id[0])+'&subs_per_page=30&client=custom_integration';
    if (req.type==='series' && /^\d+$/.test(id[1] || '') && /^\d+$/.test(id[2] || '')) url+='&type=tv&season_number='+id[1]+'&episode_number='+id[2];
    const response=ctx.ether.http.fetch({url}) as any;
    if (response.status!==200) return fail('Subtitle search failed.');
    const body=JSON.parse(response.body); if (!body.status || !Array.isArray(body.subtitles)) return fail('Subtitle search failed.');
    return {subtitles:body.subtitles.slice(0,30).filter((s:any)=>/^\/subtitle\/[0-9]+-[0-9]+\.zip$/.test(s.url || '') && /^[a-z]{2,3}$/i.test(s.language || s.lang || '')).map((s:any)=>({id:s.url,lang:(s.language || s.lang).toLowerCase(),format:'srt',name:String(s.release_name || s.name || 'SubDL').slice(0,200)}))};
  },
  subtitlesDownload(req,ctx) {
    if (!/^\/subtitle\/[0-9]+-[0-9]+\.zip$/.test(req.id) || req.format !== 'srt') return fail('Invalid subtitle selection.');
    const response=ctx.ether.http.fetch({url:'https://dl.subdl.com'+req.id,headers:{'x-api-key':ctx.ether.secret.get('apiKey') as string}}) as any;
    if (response.status!==200 || !response.bodyBase64) return fail('Subtitle download failed.');
    return {format:'srt',encoding:'utf8',data:archive(response.bodyBase64)};
  },
  test(ctx) { const key = ctx.ether.secret.get('apiKey') as string; return {ok:!!key, message:key ? 'API key saved.' : 'Enter an API key.'}; }
}});
export const handle = subdl.handle;
