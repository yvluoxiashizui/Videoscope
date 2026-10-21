#!/bin/bash
# 测「首次提交」路径：CAS 更新状态 + 投递消息 + 等 broker 确认
BASE=http://127.0.0.1:8080
U="bench$RANDOM"
P="pass1234"

curl -s -X POST $BASE/api/register -H "Content-Type: application/json" \
  -d "{\"username\":\"$U\",\"password\":\"$P\"}" > /dev/null

LOGIN=$(curl -s -X POST $BASE/api/login -H "Content-Type: application/json" \
  -d "{\"username\":\"$U\",\"password\":\"$P\"}")
TOKEN=$(echo "$LOGIN" | sed -E 's/.*"token":"([^"]+)".*/\1/')
USER_ID=$(echo "$LOGIN" | sed -E 's/.*"user_id":([0-9]+).*/\1/')
[ -z "$TOKEN" ] && { echo "登录失败"; exit 1; }

echo "=== 首次提交路径，5 次 ==="
for i in $(seq 1 5); do
  MD5=$(openssl rand -hex 16)

  # 直接插入一条待分析任务，跳过上传步骤
  mysql -u goapp -pgoapp123 videoscope -e \
    "INSERT INTO video_tasks (user_id, video_md5, object_key, filename, size, status, created_at, updated_at)
     VALUES ($USER_ID, '$MD5', 'videos/$MD5.mp4', 'bench.mp4', 1000, 0, NOW(), NOW());" 2>/dev/null

  # 一次请求同时拿状态码和耗时，避免第二次请求撞上已排队状态
  RESULT=$(curl -s -o /dev/null -w "%{http_code} %{time_total}" -X POST \
    -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
    -d "{\"md5\":\"$MD5\"}" $BASE/api/analyze)

  CODE=$(echo "$RESULT" | cut -d' ' -f1)
  TIME=$(echo "$RESULT" | cut -d' ' -f2)
  echo "第 $i 次：状态码 $CODE，耗时 ${TIME}s"
done
