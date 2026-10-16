#!/bin/sh
set -eu
: "${MODELS:?space-separated model names}"
for model in $MODELS; do
  printf '== %s\n' "$model"
  ollama pull "$model"
done
ollama list
