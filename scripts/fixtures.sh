#!/bin/sh
# Synthetic fixture generation is test preparation, not live transcoding.
set -eu
out=${1:-/opt/antennarr/fixtures}
mkdir -p "$out"
for size in 720 480; do
  case "$size" in 720) resolution=1280x720;; 480) resolution=854x480;; esac
  ffmpeg -nostdin -hide_banner -loglevel error -y -f lavfi -i "testsrc2=size=$resolution:rate=25" -f lavfi -i "sine=frequency=440:sample_rate=48000" -t 8 -c:v libx264 -preset ultrafast -pix_fmt yuv420p -g 25 -b:v 900k -c:a aac -b:a 96k -f mpegts "$out/$size.ts"
done
ffmpeg -nostdin -hide_banner -loglevel error -y -f lavfi -i 'testsrc2=size=720x576:rate=25' -f lavfi -i 'sine=frequency=880:sample_rate=48000' -t 8 -c:v mpeg2video -g 12 -b:v 1500k -c:a mp2 -b:a 128k -f mpegts "$out/mpeg2.ts"
