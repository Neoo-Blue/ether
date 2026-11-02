#!/bin/sh
# Rebuild the raw-WASM test modules. Requires a wasm32-capable clang + wasm-ld
# (Homebrew llvm works). Tests use the committed .wasm files; this script is
# only needed after editing the .c sources.
set -eu
cd "$(dirname "$0")"
CLANG="${CLANG:-clang}"
FLAGS="--target=wasm32 -O2 -nostdlib -Wl,--no-entry -Wl,--export-memory -Wl,--initial-memory=2097152 -Wl,--max-memory=67108864"
$CLANG $FLAGS -Wl,--export=alloc -Wl,--export=handle -Wl,--export=rank_key -Wl,--export=cond_match rawplugin.c -o ../rawplugin.wasm
$CLANG $FLAGS -Wl,--export=alloc -Wl,--export=handle forbidden.c -o ../forbidden.wasm
echo built ../rawplugin.wasm ../forbidden.wasm
