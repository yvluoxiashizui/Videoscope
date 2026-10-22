#!/bin/bash
# 生成测试用的语音视频：一段英文讲解循环播放，配一个极简测试画面
# 用法：make-fixture.sh [输出路径] [时长秒数]
set -e

OUT=${1:-/tmp/fixture.mp4}
DURATION=${2:-40}
SPEECH=/tmp/fixture-speech.wav

ffmpeg -y -f lavfi \
  -i "flite=text='Hello everyone, welcome to this course. Today we will learn about distributed systems and message queues.':voice=slt" \
  -ar 16000 -ac 1 "$SPEECH" 2>&1 | tail -1

# 音轨无限循环，由 -t 控制总时长。
# 画面用最小分辨率与帧率：这个文件只用于验证处理链路，
# 画质高低不影响识别与切块结果，编码时间却差好几倍。
ffmpeg -y -stream_loop -1 -i "$SPEECH" \
  -f lavfi -i "testsrc=duration=$DURATION:size=160x120:rate=5" \
  -c:v libx264 -preset ultrafast -c:a aac -shortest -t "$DURATION" "$OUT" 2>&1 | tail -1

echo "--- 结果 ---"
ls -lh "$OUT"
ffprobe -v error -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 "$OUT"
