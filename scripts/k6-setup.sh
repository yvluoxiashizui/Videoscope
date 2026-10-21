#!/bin/bash
# 为 k6 准备测试数据：注册用户 + 插入 N 个待分析任务
set -e

BASE=http://127.0.0.1:8080
U="k6setup$RANDOM"
P="pass1234"
N=${1:-50}
OUT=/tmp/k6-config.json

curl -s -X POST $BASE/api/register -H "Content-Type: application/json" \
  -d "{\"username\":\"$U\",\"password\":\"$P\"}" > /dev/null

LOGIN=$(curl -s -X POST $BASE/api/login -H "Content-Type: application/json" \
  -d "{\"username\":\"$U\",\"password\":\"$P\"}")
TOKEN=$(echo "$LOGIN" | sed -E 's/.*"token":"([^"]+)".*/\1/')
USER_ID=$(echo "$LOGIN" | sed -E 's/.*"user_id":([0-9]+).*/\1/')
[ -z "$TOKEN" ] && { echo "登录失败"; exit 1; }

echo "用户 $U (id=$USER_ID)，准备 $N 个任务"

{
  echo -n "{\"token\":\"$TOKEN\",\"tasks\":["
  first=1
  for i in $(seq 1 "$N"); do
    MD5=$(openssl rand -hex 16)
    mysql -u goapp -pgoapp123 videoscope -e \
      "INSERT INTO video_tasks (user_id, video_md5, object_key, filename, size, status, created_at, updated_at)
       VALUES ($USER_ID, '$MD5', 'videos/$MD5.mp4', 'k6.mp4', 1000, 0, NOW(), NOW());" 2>/dev/null
    if [ $first -eq 0 ]; then echo -n ","; fi
    echo -n "\"$MD5\""
    first=0
  done
  echo "]}"
} > $OUT

echo "配置写入 $OUT，任务数：$(grep -o '"' $OUT | wc -l)"
