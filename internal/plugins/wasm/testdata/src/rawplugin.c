// Test raw-WASM plugin for internal/plugins/wasm. Freestanding wasm32, no libc.
// Rebuild with testdata/src/build.sh (needs a wasm32 clang + wasm-ld).
typedef unsigned int u32;
typedef unsigned long long u64;
typedef unsigned long size_t;

void *memcpy(void *d, const void *s, size_t n) {
  unsigned char *dd = d; const unsigned char *ss = s;
  for (size_t i = 0; i < n; i++) dd[i] = ss[i];
  return d;
}
void *memset(void *d, int c, size_t n) {
  unsigned char *dd = d;
  for (size_t i = 0; i < n; i++) dd[i] = (unsigned char)c;
  return d;
}

__attribute__((import_module("ether"), import_name("log")))
void ether_log(u32 ptr, u32 len);
__attribute__((import_module("ether"), import_name("abort")))
void ether_abort(u32 code);
__attribute__((import_module("ether"), import_name("host_call")))
int ether_host_call(u32 namePtr, u32 nameLen, u32 argsPtr, u32 argsLen, u32 outPtr, u32 outCap);

static unsigned char arena[1 << 20];
static u32 bump;
__attribute__((export_name("alloc")))
u32 alloc(u32 size) {
  if (bump + size > sizeof(arena)) bump = 0;
  u32 p = (u32)(unsigned long)(arena + bump);
  bump += (size + 7) & ~7u;
  return p;
}

static int contains(const char *hay, u32 hlen, const char *needle) {
  u32 nlen = 0; while (needle[nlen]) nlen++;
  if (nlen > hlen) return 0;
  for (u32 i = 0; i + nlen <= hlen; i++) {
    u32 j = 0; while (j < nlen && hay[i + j] == needle[j]) j++;
    if (j == nlen) return 1;
  }
  return 0;
}
static u32 slen(const char *s) { u32 n = 0; while (s[n]) n++; return n; }

static char out[8192];
static u64 pack(const char *s, u32 n) {
  return ((u64)(u32)(unsigned long)s << 32) | n;
}

__attribute__((export_name("handle")))
u64 handle(u32 ptr, u32 len) {
  const char *in = (const char *)(unsigned long)ptr;
  if (contains(in, len, "/loop")) { for (;;) {} }
  if (contains(in, len, "/oom")) {
    for (;;) { if ((long)__builtin_wasm_memory_grow(0, 256) < 0) break; }
    static char oresp[128];
    const char *h = "{\"status\":200,\"headers\":{},\"body\":\"pages:";
    u32 o = 0; for (u32 i = 0; h[i]; i++) oresp[o++] = h[i];
    unsigned long pages = __builtin_wasm_memory_size(0);
    char num[16]; int nl = 0;
    if (pages == 0) num[nl++] = '0';
    while (pages > 0) { num[nl++] = (char)('0' + pages % 10); pages /= 10; }
    for (int i = nl - 1; i >= 0; i--) oresp[o++] = num[i];
    oresp[o++] = '"'; oresp[o++] = '}';
    return pack(oresp, o);
  }
  if (contains(in, len, "/abort")) { ether_abort(7); }
  ether_log((u32)(unsigned long)"raw handle", 10);
  if (contains(in, len, "/host")) {
    int n = ether_host_call((u32)(unsigned long)"kv.get", 6,
                            (u32)(unsigned long)"[\"k\"]", 5,
                            (u32)(unsigned long)out, sizeof(out));
    if (n < 0) {
      const char *e = "{\"status\":502,\"headers\":{},\"body\":\"host-error\"}";
      return pack(e, slen(e));
    }
    // Wrap the host result (a JSON string) as the response body text.
    static char resp[8192];
    const char *pre = "{\"status\":200,\"headers\":{},\"body\":\"host:\"}";
    u32 o = 0;
    // Insert raw host bytes before final quote of pre template manually:
    const char *head = "{\"status\":200,\"headers\":{},\"body\":\"host:";
    for (u32 i = 0; head[i]; i++) resp[o++] = head[i];
    for (int i = 0; i < n && o < sizeof(resp) - 4; i++) {
      if (out[i] == '"' || out[i] == '\\') continue; // keep test value plain
      resp[o++] = out[i];
    }
    resp[o++] = '"'; resp[o++] = '}';
    (void)pre;
    return pack(resp, o);
  }
  // Extract "path":"..." and echo it.
  const char *key = "\"path\":\"";
  u32 at = 0; int found = 0;
  for (u32 i = 0; i + 8 <= len; i++) {
    u32 j = 0; while (j < 8 && in[i + j] == key[j]) j++;
    if (j == 8) { at = i + 8; found = 1; break; }
  }
  static char resp[8192];
  const char *head = "{\"status\":201,\"headers\":{\"x-raw\":\"1\"},\"body\":\"raw-ok:";
  u32 o = 0;
  for (u32 i = 0; head[i]; i++) resp[o++] = head[i];
  if (found) {
    for (u32 i = at; i < len && in[i] != '"' && o < sizeof(resp) - 4; i++) resp[o++] = in[i];
  }
  resp[o++] = '"'; resp[o++] = '}';
  return pack(resp, o);
}

__attribute__((export_name("rank_key")))
long long rank_key(u32 cptr, u32 clen, u32 cfgptr, u32 cfglen) {
  const char *c = (const char *)(unsigned long)cptr;
  (void)cfgptr; (void)cfglen;
  if (contains(c, clen, "\"slow\"")) { for (volatile unsigned long i = 0; i < 50000000UL; i++) {} }
  long long score = 0;
  if (contains(c, clen, "2160")) score += 100;
  if (contains(c, clen, "\"cached\":true")) score += 10;
  return score;
}

__attribute__((export_name("cond_match")))
u32 cond_match(u32 cptr, u32 clen, u32 optr, u32 olen, u32 vptr, u32 vlen) {
  const char *c = (const char *)(unsigned long)cptr;
  const char *op = (const char *)(unsigned long)optr;
  const char *v = (const char *)(unsigned long)vptr;
  if (olen == 8 && contains(op, olen, "contains")) {
    // value is a JSON string; strip quotes.
    if (vlen >= 2 && v[0] == '"') { v++; vlen -= 2; }
    if (vlen > clen) return 0;
    for (u32 i = 0; i + vlen <= clen; i++) {
      u32 j = 0; while (j < vlen && c[i + j] == v[j]) j++;
      if (j == vlen) return 1;
    }
    return 0;
  }
  return 0;
}
