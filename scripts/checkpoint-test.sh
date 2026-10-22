#!/bin/bash
# 验证断点恢复：第一次完整处理，重置状态后第二次应当从快照继续
BASE=http://127.0.0.1:8080
U="ckpt$RANDOM"
P="pass1234"
VIDEO=${1:-/tmp/fixture.mp4}

[ -f "$VIDEO" ] || { echo "视频不存在: $VIDEO"; exit 1; }

curl -s -X POST $BASE/api/register -H "Content-Type: application/json" \
  -d "{\"username\":\"$U\",\"password\":\"$P\"}" > /dev/null
TOKEN=$(curl -s -X POST $BASE/api/login -H "Content-Type: application/json" \
  -d "{\"username\":\"$U\",\"password\":\"$P\"}" | sed -E 's/.*"token":"([^"]+)".*/\1/')
[ -z "$TOKEN" ] && { echo "登录失败"; exit 1; }

MD5=$(md5sum $VIDEO | cut -d' ' -f1)
curl -s -X POST $BASE/api/upload/chunk -H "Authorization: Bearer $TOKEN" \
  -F "md5=$MD5" -F "index=0" -F "total=1" -F "file=@$VIDEO" > /dev/null
MERGE=$(curl -s -X POST $BASE/api/upload/merge -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"md5\":\"$MD5\",\"total\":1,\"ext\":\"mp4\",\"filename\":\"ckpt.mp4\"}")
TASK_ID=$(echo "$MERGE" | sed -E 's/.*"task_id":([0-9]+).*/\1/')
echo "task_id=$TASK_ID"

wait_done() {
  for i in $(seq 1 120); do
    st=$(mysql -u goapp -pgoapp123 videoscope -N -B \
      -e "SELECT status FROM video_tasks WHERE id=$TASK_ID;" 2>/dev/null)
    if [ "$st" = "3" ] || [ "$st" = "4" ]; then echo "$st"; return; fi
    sleep 2
  done
  echo "timeout"
}

echo "=== 第一次：完整处理 ==="
curl -s -X POST $BASE/api/analyze -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" -d "{\"md5\":\"$MD5\"}" > /dev/null
echo "结果: $(wait_done)"

SNAPSHOT_LEN=$(mysql -u goapp -pgoapp123 videoscope -N -B \
  -e "SELECT CHAR_LENGTH(context_json) FROM video_transcriptions WHERE task_id=$TASK_ID;" 2>/dev/null)
echo "上下文快照长度: $SNAPSHOT_LEN"

echo
echo "=== 第二次：重置状态后重跑 ==="
mysql -u goapp -pgoapp123 videoscope -e \
  "UPDATE video_tasks SET status=0, err_msg='' WHERE id=$TASK_ID;" 2>/dev/null

curl -s -X POST $BASE/api/analyze -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" -d "{\"md5\":\"$MD5\"}" > /dev/null
echo "结果: $(wait_done)"
