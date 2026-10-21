#!/bin/bash
# 阶段四验证：上传 -> 合并建任务 -> 提交分析 -> 限流
BASE=http://127.0.0.1:8080
U="tester$RANDOM"
P="pass1234"

echo "=== 注册并登录 $U ==="
curl -s -X POST $BASE/api/register -H "Content-Type: application/json" \
  -d "{\"username\":\"$U\",\"password\":\"$P\"}" > /dev/null
LOGIN=$(curl -s -X POST $BASE/api/login -H "Content-Type: application/json" \
  -d "{\"username\":\"$U\",\"password\":\"$P\"}")
TOKEN=$(echo "$LOGIN" | sed -E 's/.*"token":"([^"]+)".*/\1/')
[ -z "$TOKEN" ] && { echo "登录失败"; exit 1; }
echo "ok"

echo
echo "=== 造文件并上传两片 ==="
dd if=/dev/urandom of=/tmp/d0.bin bs=1M count=5 2>/dev/null
dd if=/dev/urandom of=/tmp/d1.bin bs=1M count=5 2>/dev/null
cat /tmp/d0.bin /tmp/d1.bin > /tmp/dfull.bin
MD5=$(md5sum /tmp/dfull.bin | cut -d' ' -f1)
curl -s -X POST $BASE/api/upload/chunk -H "Authorization: Bearer $TOKEN" \
  -F "md5=$MD5" -F "index=0" -F "total=2" -F "file=@/tmp/d0.bin" > /dev/null
curl -s -X POST $BASE/api/upload/chunk -H "Authorization: Bearer $TOKEN" \
  -F "md5=$MD5" -F "index=1" -F "total=2" -F "file=@/tmp/d1.bin" > /dev/null
echo "已上传，md5=$MD5"

echo
echo "=== 合并（应返回 task_id） ==="
MERGE=$(curl -s -X POST $BASE/api/upload/merge -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"md5\":\"$MD5\",\"total\":2,\"ext\":\"mp4\",\"filename\":\"demo.mp4\"}")
echo "$MERGE"
TASKID=$(echo "$MERGE" | sed -E 's/.*"task_id":([0-9]+).*/\1/')

echo
echo "=== 提交分析（应进入排队） ==="
curl -s -X POST $BASE/api/analyze -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" -d "{\"md5\":\"$MD5\"}"
echo

echo
echo "=== 立刻再提交一次（任务已排队，应提示处理中） ==="
curl -s -X POST $BASE/api/analyze -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" -d "{\"md5\":\"$MD5\"}"
echo

echo
echo "=== 换一个没上传过的 md5（应 404） ==="
curl -s -X POST $BASE/api/analyze -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" -d "{\"md5\":\"00000000000000000000000000000000\"}"
echo

echo
echo "=== 连发 20 次，观察限流 ==="
PASS=0
LIMITED=0
for i in $(seq 1 20); do
  CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST $BASE/api/analyze \
    -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
    -d "{\"md5\":\"$MD5\"}")
  if [ "$CODE" = "429" ]; then
    LIMITED=$((LIMITED+1))
  else
    PASS=$((PASS+1))
  fi
done
echo "放行 $PASS 次，限流 $LIMITED 次"

echo
echo "=== 队列状态 ==="
wsl -u root rabbitmqctl list_queues name messages 2>/dev/null | grep videoscope || true
