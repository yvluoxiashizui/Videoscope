#!/bin/bash
# 阶段六验证：上传真实视频 -> 处理流水线 -> 转录落库
BASE=http://127.0.0.1:8080
U="tester$RANDOM"
P="pass1234"
VIDEO=/tmp/testvideo.mp4

echo "=== 注册登录 ==="
curl -s -X POST $BASE/api/register -H "Content-Type: application/json" \
  -d "{\"username\":\"$U\",\"password\":\"$P\"}" > /dev/null
TOKEN=$(curl -s -X POST $BASE/api/login -H "Content-Type: application/json" \
  -d "{\"username\":\"$U\",\"password\":\"$P\"}" | sed -E 's/.*"token":"([^"]+)".*/\1/')
[ -z "$TOKEN" ] && { echo "登录失败"; exit 1; }
echo "ok"

MD5=$(md5sum $VIDEO | cut -d' ' -f1)
SIZE=$(stat -c%s $VIDEO)
echo "视频 md5=$MD5 size=$SIZE"

echo
echo "=== 上传单片（文件小于 5MB，total=1） ==="
curl -s -X POST $BASE/api/upload/chunk -H "Authorization: Bearer $TOKEN" \
  -F "md5=$MD5" -F "index=0" -F "total=1" -F "file=@$VIDEO"
echo

echo "=== 合并 ==="
MERGE=$(curl -s -X POST $BASE/api/upload/merge -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"md5\":\"$MD5\",\"total\":1,\"ext\":\"mp4\",\"filename\":\"testvideo.mp4\"}")
echo "$MERGE"
TASKID=$(echo "$MERGE" | sed -E 's/.*"task_id":([0-9]+).*/\1/')

echo
echo "=== 提交分析 ==="
curl -s -X POST $BASE/api/analyze -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" -d "{\"md5\":\"$MD5\"}"
echo

echo
echo "=== 等待处理（最多 60 秒） ==="
for i in $(seq 1 30); do
  STATUS=$(mysql -u goapp -pgoapp123 videoscope -N -B \
    -e "SELECT status FROM video_tasks WHERE id=$TASKID;" 2>/dev/null)
  if [ "$STATUS" = "3" ] || [ "$STATUS" = "4" ]; then
    echo "第 ${i} 次查询：status=$STATUS"
    break
  fi
  sleep 2
done

echo
echo "=== 任务结果 ==="
mysql -u goapp -pgoapp123 videoscope -e \
  "SELECT id, status, size, err_msg FROM video_tasks WHERE id=$TASKID;" 2>/dev/null

echo
echo "=== 转录结果 ==="
mysql -u goapp -pgoapp123 videoscope -e \
  "SELECT task_id, word_count, LEFT(content,120) AS content_head FROM video_transcriptions WHERE task_id=$TASKID;" 2>/dev/null
