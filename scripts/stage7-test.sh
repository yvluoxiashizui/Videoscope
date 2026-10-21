#!/bin/bash
# 阶段七验证：关键帧抽取 + 多模态上下文
BASE=http://127.0.0.1:8080
U="tester$RANDOM"
P="pass1234"
VIDEO=${1:-/tmp/testvideo.mp4}

curl -s -X POST $BASE/api/register -H "Content-Type: application/json" \
  -d "{\"username\":\"$U\",\"password\":\"$P\"}" > /dev/null
TOKEN=$(curl -s -X POST $BASE/api/login -H "Content-Type: application/json" \
  -d "{\"username\":\"$U\",\"password\":\"$P\"}" | sed -E 's/.*"token":"([^"]+)".*/\1/')
[ -z "$TOKEN" ] && { echo "登录失败"; exit 1; }

MD5=$(md5sum $VIDEO | cut -d' ' -f1)
TOTAL=$(python3 -c "
import os,math
print(max(1, math.ceil(os.path.getsize('$VIDEO')/5242880)))
")
echo "视频 $(stat -c%s $VIDEO) 字节，切 $TOTAL 片"

rm -f /tmp/v7chunk_*
split -b 5M -d -a 3 $VIDEO /tmp/v7chunk_
CHUNKS=(/tmp/v7chunk_*)
for i in "${!CHUNKS[@]}"; do
  curl -s -X POST $BASE/api/upload/chunk -H "Authorization: Bearer $TOKEN" \
    -F "md5=$MD5" -F "index=$i" -F "total=$TOTAL" -F "file=@${CHUNKS[$i]}" > /dev/null
done

MERGE=$(curl -s -X POST $BASE/api/upload/merge -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"md5\":\"$MD5\",\"total\":$TOTAL,\"ext\":\"mp4\",\"filename\":\"v7.mp4\"}")
TASKID=$(echo "$MERGE" | sed -E 's/.*"task_id":([0-9]+).*/\1/')
echo "task_id=$TASKID"

curl -s -X POST $BASE/api/analyze -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" -d "{\"md5\":\"$MD5\"}" > /dev/null

for i in $(seq 1 60); do
  STATUS=$(mysql -u goapp -pgoapp123 videoscope -N -B \
    -e "SELECT status FROM video_tasks WHERE id=$TASKID;" 2>/dev/null)
  if [ "$STATUS" = "3" ] || [ "$STATUS" = "4" ]; then
    echo "处理结束 status=$STATUS"
    break
  fi
  sleep 2
done
