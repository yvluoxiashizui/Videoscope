#!/bin/bash
# 测语音识别接口在不同并发数下的耗时与失败率
# 用法：asr-bench.sh [并发档位...]，默认 1 3 5 8
set -e

BASE_URL=${VIDEOSCOPE_ASR_BASE_URL:?未设置 VIDEOSCOPE_ASR_BASE_URL}
API_KEY=${VIDEOSCOPE_ASR_API_KEY:?未设置 VIDEOSCOPE_ASR_API_KEY}
MODEL=${VIDEOSCOPE_ASR_MODEL:-FunAudioLLM/SenseVoiceSmall}

AUDIO_DIR=/tmp/asr-bench
SEGMENTS=12
SEG_LEN=30
CODES=/tmp/asr-codes.txt

if [ ! -d "$AUDIO_DIR" ] || [ "$(ls -1 $AUDIO_DIR/*.wav 2>/dev/null | wc -l)" -lt "$SEGMENTS" ]; then
  rm -rf "$AUDIO_DIR"; mkdir -p "$AUDIO_DIR"

  ffmpeg -y -f lavfi \
    -i "flite=text='Hello everyone, welcome to this course. Today we will learn about distributed systems and message queues.':voice=slt" \
    -ar 16000 -ac 1 /tmp/asr-base.wav 2>/dev/null

  ffmpeg -y -stream_loop -1 -i /tmp/asr-base.wav -t $((SEGMENTS * SEG_LEN)) \
    -ar 16000 -ac 1 /tmp/asr-long.wav 2>/dev/null

  ffmpeg -y -i /tmp/asr-long.wav -f segment -segment_time "$SEG_LEN" \
    -c copy "$AUDIO_DIR/seg_%03d.wav" 2>/dev/null

  echo "已准备 $(ls -1 $AUDIO_DIR/*.wav | wc -l) 段音频，每段 ${SEG_LEN} 秒"
fi

FILES=("$AUDIO_DIR"/*.wav)
TOTAL=${#FILES[@]}

for P in ${@:-1 3 5 8}; do
  : > "$CODES"
  START=$(date +%s%N)
  n=0

  for f in "${FILES[@]}"; do
    (
      code=$(curl -s -o /dev/null -w "%{http_code}" --max-time 180 \
        -X POST "$BASE_URL/v1/audio/transcriptions" \
        -H "Authorization: Bearer $API_KEY" \
        -F "file=@$f" -F "model=$MODEL")
      echo "$code" >> "$CODES"
    ) &
    n=$((n + 1))
    # 每启动 P 个就等这一批结束，形成固定大小的并发窗口
    if [ $((n % P)) -eq 0 ]; then wait; fi
  done
  wait

  END=$(date +%s%N)
  ELAPSED=$(( (END - START) / 1000000 ))

  OK=$(grep -c '^200$' "$CODES" 2>/dev/null || echo 0)
  DONE=$(wc -l < "$CODES")
  FAIL=$((DONE - OK))

  echo "并发 $P：成功 $OK / $TOTAL，失败 $FAIL，总耗时 ${ELAPSED}ms"
done
