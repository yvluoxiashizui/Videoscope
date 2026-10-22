#!/bin/bash
# 验证 RAG：处理视频、建立索引、追问
BASE=http://127.0.0.1:8080
U="rag$RANDOM"
P="pass1234"
VIDEO=${1:-/tmp/talk3.mp4}

[ -f "$VIDEO" ] || { echo "视频不存在: $VIDEO"; exit 1; }

curl -s -X POST $BASE/api/register -H "Content-Type: application/json" \
  -d "{\"username\":\"$U\",\"password\":\"$P\"}" > /dev/null
LOGIN=$(curl -s -X POST $BASE/api/login -H "Content-Type: application/json" \
  -d "{\"username\":\"$U\",\"password\":\"$P\"}")
TOKEN=$(echo "$LOGIN" | sed -E 's/.*"token":"([^"]+)".*/\1/')
[ -z "$TOKEN" ] && { echo "登录失败"; exit 1; }

MD5=$(md5sum $VIDEO | cut -d' ' -f1)
TOTAL=$(python3 -c "import os,math;print(max(1,math.ceil(os.path.getsize('$VIDEO')/5242880)))")
echo "视频 md5=$MD5 切 $TOTAL 片"

rm -f /tmp/ragchunk_*
split -b 5M -d -a 3 $VIDEO /tmp/ragchunk_
CHUNKS=(/tmp/ragchunk_*)
for i in "${!CHUNKS[@]}"; do
  curl -s -X POST $BASE/api/upload/chunk -H "Authorization: Bearer $TOKEN" \
    -F "md5=$MD5" -F "index=$i" -F "total=$TOTAL" -F "file=@${CHUNKS[$i]}" > /dev/null
done

MERGE=$(curl -s -X POST $BASE/api/upload/merge -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"md5\":\"$MD5\",\"total\":$TOTAL,\"ext\":\"mp4\",\"filename\":\"rag.mp4\"}")
TASK_ID=$(echo "$MERGE" | sed -E 's/.*"task_id":([0-9]+).*/\1/')
echo "task_id=$TASK_ID"

curl -s -X POST $BASE/api/analyze -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" -d "{\"md5\":\"$MD5\"}" > /dev/null

echo "等待处理..."
for i in $(seq 1 90); do
  STATUS=$(mysql -u goapp -pgoapp123 videoscope -N -B \
    -e "SELECT status FROM video_tasks WHERE id=$TASK_ID;" 2>/dev/null)
  if [ "$STATUS" = "3" ] || [ "$STATUS" = "4" ]; then
    echo "处理结束 status=$STATUS"
    break
  fi
  sleep 2
done

echo
echo "=== 索引块 ==="
mysql -u goapp -pgoapp123 videoscope -e \
  "SELECT chunk_index, ROUND(start_seconds) AS s, ROUND(end_seconds) AS e, CHAR_LENGTH(content) AS chars, CHAR_LENGTH(embedding) AS emb_len FROM video_chunks WHERE task_id=$TASK_ID ORDER BY chunk_index;" 2>/dev/null

echo
echo "=== 追问：这个视频讲了什么主题 ==="
curl -s -X POST $BASE/api/task/$TASK_ID/ask -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"question":"这个视频的主要内容是什么？"}' | head -c 800
echo
