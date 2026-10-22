#!/bin/bash
# 生成测试用的语音视频：一段英文讲解 + 循环音轨 + 测试画面
set -e

OUT=${1:-/tmp/fixture.mp4}
SPEECH=/tmp/fixture-speech.wav

ffmpeg -y -f lavfi \
  -i "flite=text='Hello everyone, welcome to this course. Today we will learn about distributed systems and message queues.':voice=slt" \
  -ar 16000 -ac 1 "$SPEECH" 2>&1 | tail -1

# 循环音轨拼到四十秒左右，够切出两个上下文窗口
ffmpeg -y -stream_loop 3 -i "$SPEECH" \
  -f lavfi -i "testsrc=duration=40:size=320x240:rate=10" \
  -c:v libx264 -preset ultrafast -c:a aac -shortest "$OUT" 2>&1 | tail -1

echo "--- 结果 ---"
ls -lh "$OUT"
ffprobe -v error -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 "$OUT"
