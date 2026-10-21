#!/bin/bash
# 阶段一与阶段二的端到端验证
BASE=http://127.0.0.1:8080
U="tester$RANDOM"
P="pass1234"

echo "=== 1. 注册 $U ==="
curl -s -X POST $BASE/api/register -H "Content-Type: application/json" \
  -d "{\"username\":\"$U\",\"password\":\"$P\"}"
echo

echo "=== 2. 登录 ==="
LOGIN=$(curl -s -X POST $BASE/api/login -H "Content-Type: application/json" \
  -d "{\"username\":\"$U\",\"password\":\"$P\"}")
echo "$LOGIN"
TOKEN=$(echo "$LOGIN" | sed -E 's/.*"token":"([^"]+)".*/\1/')
if [ -z "$TOKEN" ] || [ "$TOKEN" = "$LOGIN" ]; then
  echo "!! 取 token 失败，终止"
  exit 1
fi

echo "=== 3. 未带 token 访问受保护接口 ==="
curl -s -o /dev/null -w "状态码 %{http_code}\n" $BASE/api/me

echo "=== 4. 造两个 5MB 分片 ==="
dd if=/dev/urandom of=/tmp/c0.bin bs=1M count=5 2>/dev/null
dd if=/dev/urandom of=/tmp/c1.bin bs=1M count=5 2>/dev/null
cat /tmp/c0.bin /tmp/c1.bin > /tmp/full.bin
MD5=$(md5sum /tmp/full.bin | cut -d' ' -f1)
echo "整文件 md5=$MD5 大小=$(stat -c%s /tmp/full.bin)"

echo "=== 5. 上传分片 0 ==="
curl -s -X POST $BASE/api/upload/chunk -H "Authorization: Bearer $TOKEN" \
  -F "md5=$MD5" -F "index=0" -F "total=2" -F "file=@/tmp/c0.bin"
echo

echo "=== 6. 重复上传分片 0（验证幂等） ==="
curl -s -X POST $BASE/api/upload/chunk -H "Authorization: Bearer $TOKEN" \
  -F "md5=$MD5" -F "index=0" -F "total=2" -F "file=@/tmp/c0.bin"
echo

echo "=== 7. 查询状态（此时应只有 1 片） ==="
curl -s "$BASE/api/upload/status?md5=$MD5&total=2" -H "Authorization: Bearer $TOKEN"
echo

echo "=== 8. 分片未齐就合并（应被拒绝） ==="
curl -s -X POST $BASE/api/upload/merge -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"md5\":\"$MD5\",\"total\":2,\"ext\":\"mp4\"}"
echo

echo "=== 9. 上传分片 1 ==="
curl -s -X POST $BASE/api/upload/chunk -H "Authorization: Bearer $TOKEN" \
  -F "md5=$MD5" -F "index=1" -F "total=2" -F "file=@/tmp/c1.bin"
echo

echo "=== 10. 合并 ==="
curl -s -X POST $BASE/api/upload/merge -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"md5\":\"$MD5\",\"total\":2,\"ext\":\"mp4\"}"
echo

echo "=== 11. 再次合并（验证幂等，应返回 reused） ==="
curl -s -X POST $BASE/api/upload/merge -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"md5\":\"$MD5\",\"total\":2,\"ext\":\"mp4\"}"
echo

echo "=== 12. 合并后查状态（分片记录应已清空） ==="
curl -s "$BASE/api/upload/status?md5=$MD5&total=2" -H "Authorization: Bearer $TOKEN"
echo

echo "=== 13. 非法 md5 ==="
curl -s "$BASE/api/upload/status?md5=notamd5" -H "Authorization: Bearer $TOKEN"
echo

echo "=== 14. index 越界 ==="
curl -s -X POST $BASE/api/upload/chunk -H "Authorization: Bearer $TOKEN" \
  -F "md5=$MD5" -F "index=5" -F "total=2" -F "file=@/tmp/c0.bin"
echo
