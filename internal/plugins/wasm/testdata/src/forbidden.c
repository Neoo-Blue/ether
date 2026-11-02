// Test module with a forbidden (WASI) import. Loading must be refused.
typedef unsigned int u32;
typedef unsigned long long u64;
__attribute__((import_module("wasi_snapshot_preview1"), import_name("fd_write")))
int fd_write(u32 a, u32 b, u32 c, u32 d);
__attribute__((export_name("alloc")))
u32 alloc(u32 n) { return 1024; }
__attribute__((export_name("handle")))
u64 handle(u32 p, u32 l) { fd_write(1, 0, 0, 0); return 0; }
