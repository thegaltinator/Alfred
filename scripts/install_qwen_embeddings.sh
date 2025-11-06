#!/usr/bin/env bash
# Download Qwen/Qwen3-Embedding-0.6B GGUF from Hugging Face and stage it for Alfred.
set -Eeuo pipefail

MODEL_FILE="${MODEL_FILE:-Qwen3-Embedding-0.6B-f16.gguf}"
MODEL_REPO_URL="https://huggingface.co/Qwen/Qwen3-Embedding-0.6B-GGUF/resolve/main"
TARGET_DIR="${TARGET_DIR:-${HOME}/Library/Application Support/Alfred/Models}"
TARGET_PATH="${TARGET_DIR}/${MODEL_FILE}"

echo "📥 Preparing to download ${MODEL_FILE} from Hugging Face…"
echo "    Target directory: ${TARGET_DIR}"

mkdir -p "${TARGET_DIR}"

DOWNLOAD_URL="${MODEL_REPO_URL}/${MODEL_FILE}?download=1"

if command -v curl >/dev/null 2>&1; then
  echo "➡️  Using curl to fetch ${DOWNLOAD_URL}"
  curl -fL "${DOWNLOAD_URL}" -o "${TARGET_PATH}"
elif command -v wget >/dev/null 2>&1; then
  echo "➡️  Using wget to fetch ${DOWNLOAD_URL}"
  wget -O "${TARGET_PATH}" "${DOWNLOAD_URL}"
else
  echo "❌ Neither curl nor wget is available. Install one of them to download the model." >&2
  exit 1
fi

echo "✅ Download complete: ${TARGET_PATH}"
echo "   Export ALFRED_EMBED_MODEL_PATH=\"${TARGET_PATH}\" if Alfred runs from a different account."
