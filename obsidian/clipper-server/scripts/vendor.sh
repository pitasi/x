#!/usr/bin/env bash
# Rebuilds server/vendor/api.mjs from the pinned obsidian-clipper tag.
# Unlike upstream's build:api (platform-neutral, defuddle/dayjs external — needs
# a bundler to consume), we build a self-contained node bundle: no externals,
# CJS interop resolved at build time. Caller still must polyfill DOMParser first.
set -euo pipefail

TAG=1.7.1
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

git clone --depth 1 --branch "$TAG" https://github.com/obsidianmd/obsidian-clipper.git "$TMP/clipper"
cd "$TMP/clipper"
# upstream 1.7.1 lockfile is out of sync with its package.json, npm ci refuses
npm install --no-audit --no-fund

npx esbuild src/api.ts \
  --bundle \
  --platform=node \
  --format=esm \
  --define:DEBUG_MODE=false \
  --alias:webextension-polyfill=./src/utils/cli-stubs.ts \
  --outfile=dist/api-node.mjs

mkdir -p "$ROOT/server/vendor"
# upstream bug: clip() feeds doc.documentElement to Defuddle, which returns
# empty title/content for an Element under linkedom; it needs the Document
sed 's/const documentElement = doc.documentElement || doc;/const documentElement = doc;/' \
  dist/api-node.mjs > "$ROOT/server/vendor/api.mjs"
echo "vendored obsidian-clipper@$TAG -> server/vendor/api.mjs"
