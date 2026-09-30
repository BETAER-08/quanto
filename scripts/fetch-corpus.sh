#!/usr/bin/env bash
set -u

root="$(cd "$(dirname "$0")/.." && pwd)"
dest="$root/testdata/corpus"
mkdir -p "$dest"

targets=(
  rust-lang/rust/master/.github/workflows/ci.yml
  nodejs/node/main/.github/workflows/build-tarball.yml
  nodejs/node/main/.github/workflows/test-linux.yml
  prometheus/prometheus/main/.github/workflows/ci.yml
  hashicorp/terraform/main/.github/workflows/checks.yml
  actions/checkout/main/.github/workflows/test.yml
  ollama/ollama/main/.github/workflows/test.yaml
  fastapi/fastapi/master/.github/workflows/test.yml
  vercel/next.js/canary/.github/workflows/build_and_test.yml
  python/cpython/main/.github/workflows/build.yml
  python/cpython/main/.github/workflows/reusable-docs.yml
  python/cpython/main/.github/workflows/reusable-macos.yml
  numpy/numpy/main/.github/workflows/wheels.yml
  numpy/numpy/main/.github/workflows/linux.yml
  nodejs/node/main/.github/workflows/stale.yml
  django/django/main/.github/workflows/schedule_tests.yml
  django/django/main/.github/workflows/tests.yml
  getsentry/sentry/master/.github/workflows/backend.yml
  microsoft/vscode/main/.github/workflows/pr.yml
  facebook/react/main/.github/workflows/runtime_build_and_test.yml
  tokio-rs/tokio/master/.github/workflows/ci.yml
  pytorch/pytorch/main/.github/workflows/lint.yml
  pytorch/pytorch/main/.github/workflows/pull.yml
  tensorflow/tensorflow/master/.github/workflows/stale-issues.yml
  ant-design/ant-design/master/.github/workflows/test.yml
  vuejs/core/main/.github/workflows/ci.yml
  home-assistant/core/dev/.github/workflows/ci.yaml
  home-assistant/core/dev/.github/workflows/stale.yml
  cli/cli/trunk/.github/workflows/go.yml
  astral-sh/ruff/main/.github/workflows/ci.yaml
  astral-sh/uv/main/.github/workflows/ci.yml
  pola-rs/polars/main/.github/workflows/test-python.yml
  pandas-dev/pandas/main/.github/workflows/unit-tests.yml
  scikit-learn/scikit-learn/main/.github/workflows/wheels.yml
  docker/compose/main/.github/workflows/ci.yml
  electron/electron/main/.github/workflows/build.yml
  microsoft/TypeScript/main/.github/workflows/ci.yml
  sveltejs/svelte/main/.github/workflows/ci.yml
  withastro/astro/main/.github/workflows/ci.yml
  gohugoio/hugo/master/.github/workflows/test.yml
  caddyserver/caddy/master/.github/workflows/ci.yml
  nushell/nushell/main/.github/workflows/ci.yml
  langchain-ai/langchain/master/.github/workflows/_test.yml
  apache/echarts/master/.github/workflows/ci.yml
  PaddlePaddle/Paddle/develop/.github/workflows/CI.yml
  bitcoin-core/secp256k1/master/.github/workflows/ci.yml
  bitcoin/bitcoin/master/.github/workflows/ci.yml
  home-assistant/supervisor/main/.github/workflows/builder.yml
  home-assistant/frontend/dev/.github/workflows/ci.yaml
)

count=0
for target in "${targets[@]}"; do
  owner="${target%%/*}"
  rest="${target#*/}"
  repo="${rest%%/*}"
  base="${target##*/}"
  out="$dest/${owner}_${repo}_${base}"
  if curl -fsSL --max-time 20 -o "$out.tmp" "https://raw.githubusercontent.com/$target"; then
    mv "$out.tmp" "$out"
    count=$((count + 1))
  else
    rm -f "$out.tmp"
    echo "miss $target"
  fi
done

echo "fetched $count files into $dest"
