#!/bin/bash
# 验证 SSE 推送：订阅任务事件，观察状态流转
BASE=http://127.0.0.1:8080
U="sse$RANDOM"
P="pass1234"

curl -s -X POST $BASE/api/register -H "Content-Type: application/json" \
  -d "{\"username\":\"$U\",\"password\":\"$P\"}" > /dev/null

LOGIN=$(curl -s -X POST $BASE/api/login -H "Content-Type: application/json" \
  -d "{\"username\":\"$U\",\"password\":\"$P\"}")
TOKEN=$(echo "$LOGIN" | sed -E 's/.*"token":"([^"]+)".*/\1/')
USER_ID=$(echo "$LOGIN" | sed -E 's/.*"user_id":([0-9]+).*/\1/')
[ -z "$TOKEN" ] && { echo "登录失败"; exit 1; }

MD5=$(openssl rand -hex 16)
mysql -u goapp -pgoapp123 videoscope -e \
  "INSERT INTO video_tasks (user_id, video_md5, object_key, filename, size, status, created_at, updated_at)
   VALUES ($USER_ID, '$MD5', 'videos/$MD5.mp4', 'sse.mp4', 1000, 0, NOW(), NOW());" 2>/dev/null
TASK_ID=$(mysql -u goapp -pgoapp123 videoscope -N -B \
  -e "SELECT id FROM video_tasks WHERE video_md5='$MD5';" 2>/dev/null)

echo "任务 $TASK_ID 已创建，开始订阅 SSE"

# 后台订阅，最多挂 12 秒
timeout 12 curl -sN -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/task/$TASK_ID/events" > /tmp/sse-out.txt 2>&1 &
SSE_PID=$!

sleep 2
echo "提交分析..."
curl -s -X POST $BASE/api/analyze -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" -d "{\"md5\":\"$MD5\"}" > /dev/null

wait $SSE_PID 2>/dev/null

echo "=== 收到的事件 ==="
cat /tmp/sse-out.txt
