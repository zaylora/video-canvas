#!/usr/bin/env bash
# 把 docs/research/open-ai-canvas-assets 里的预设图片导出到 public/presets（设计稿 6.13）。
# 需要带 libwebp 的 ffmpeg。一次性脚本，不进运行时；预设图片更新时重跑一遍。
#   style   风格封面，压到 480 宽
#   motion  运镜示意图，原样复制（源文件是单帧静态图，不是动图）
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
src="$root/docs/research/open-ai-canvas-assets"
dst="$root/web/public/presets"

mkdir -p "$dst/style" "$dst/motion"

resize() { # 源目录 目标目录 宽度
  for f in "$1"/*.webp; do
    ffmpeg -v error -y -i "$f" -vf "scale=$3:-2" -c:v libwebp -quality 78 "$2/$(basename "$f")"
  done
}

resize "$src/style" "$dst/style" 480

cp "$src"/motion/*.webp "$dst/motion/"

echo "导出完成：$(find "$dst" -type f | wc -l | tr -d ' ') 个文件，$(du -sh "$dst" | cut -f1)"
