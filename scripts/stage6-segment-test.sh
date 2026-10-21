#!/bin/bash
# 阶段六补充验证：多分片上传 + 长视频分段转写
BASE=http://127.0.0.1:8080
U="tester$RANDOM"
P="pass1234"
VIDEO=${1:-/tmp/testvideo.mp4}

echo "=== 注册登录 ==="
curl -s -X POST $BASE/api/register -H "Content-Type: application/json" \
  -d "{\"username\":\"$U\",\"password\":\"$P\"}" > /dev/null
TOKEN=$(curl -s -X POST $BASE/api/login -H "Content-Type: application/json" \
  -d "{\"username\":\"$U\",\"password\":\"$P\"}" | sed -E 's/.*"token":"([^"]+)".*/\1/')
[ -z "$TOKEN" ] && { echo "登录失败"; exit 1; }

MD5=$(md5sum $VIDEO | cut -d' ' -f1)
echo "视频 md5=$MD5 size=$(stat -c%s $VIDEO)"

echo
echo "=== 切分片并上传 ==="
rm -f /tmp/bigchunk_*
split -b 5M -d -a 3 $VIDEO /tmp/bigchunk_
CHUNKS=(/tmp/bigchunk_*)
TOTAL=${#CHUNKS[@]}
echo "共 $TOTAL 片"

for i in "${!CHUNKS[@]}"; do
  RESP=$(curl -s -X POST $BASE/api/upload/chunk -H "Authorization: Bearer $TOKEN" \
    -F "md5=$MD5" -F "index=$i" -F "total=$TOTAL" -F "file=@${CHUNKS[$i]}")
  echo "  片 $i: $(stat -c%s ${CHUNKS[$i]}) 字节 -> $RESP"
done

echo
echo "=== 合并 ==="
MERGE=$(curl -s -X POST $BASE/api/upload/merge -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"md5\":\"$MD5\",\"total\":$TOTAL,\"ext\":\"mp4\",\"filename\":\"bigvideo.mp4\"}")
echo "$MERGE"
TASKID=$(echo "$MERGE" | sed -E 's/.*"task_id":([0-9]+).*/\1/')

echo
echo "=== 提交分析 ==="
curl -s -X POST $BASE/api/analyze -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" -d "{\"md5\":\"$MD5\"}"
echo

echo
echo "=== 等待处理 ==="
for i in $(seq 1 60); do
  STATUS=$(mysql -u goapp -pgoapp123 videoscope -N -B \
    -e "SELECT status FROM video_tasks WHERE id=$TASKID;" 2>/dev/null)
  if [ "$STATUS" = "3" ] || [ "$STATUS" = "4" ]; then
    echo "第 $i 次查询 status=$STATUS"
    break
  fi
  sleep 2
done

echo
echo "=== 任务状态 ==="
mysql -u goapp -pgoapp123 videoscope -e \
  "SELECT id, status, size, err_msg FROM video_tasks WHERE id=$TASKID;" 2>/dev/null

echo
echo "=== 转录结果（应为两段） ==="
mysql -u goapp -pgoapp123 videoscope -e \
  "SELECT task_id, word_count, CHAR_LENGTH(content) AS chars FROM video_transcriptions WHERE task_id=$TASKID;" 2>/dev/null
mysql -u goapp -pgoapp123 videoscope -e \
  "SELECT content FROM video_transcriptions WHERE task_id=$TASKID\G" 2>/dev/null
