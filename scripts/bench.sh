#!/bin/bash
# 分析提交接口的响应耗时基准测试
BASE=http://127.0.0.1:8080
U="bench$RANDOM"
P="pass1234"

curl -s -X POST $BASE/api/register -H "Content-Type: application/json" \
  -d "{\"username\":\"$U\",\"password\":\"$P\"}" > /dev/null

TOKEN=$(curl -s -X POST $BASE/api/login -H "Content-Type: application/json" \
  -d "{\"username\":\"$U\",\"password\":\"$P\"}" | sed -E 's/.*"token":"([^"]+)".*/\1/')
[ -z "$TOKEN" ] && { echo "登录失败"; exit 1; }

# 用一个已处理过的任务，提交会走「已完成」的快速分支
MD5=${1:-923f29b144f442e8c3fa6ddf0c2d16da}

echo "=== /api/analyze 基准测试：10 请求，2 并发（不超过令牌桶容量）==="
~/go/bin/hey -n 10 -c 2 -m POST \
  -H "Authorization: Bearer $TOKEN" \
  -T "application/json" \
  -d "{\"md5\":\"$MD5\"}" \
  $BASE/api/analyze 2>&1 | tail -22
