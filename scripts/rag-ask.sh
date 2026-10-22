#!/bin/bash
# 用已有任务验证多块检索：把任务转给新用户后提问
BASE=http://127.0.0.1:8080
TASK_ID=${1:?用法: rag-ask.sh <task_id> <问题>}
QUESTION=${2:-"这个视频讲了什么"}

U="ask$RANDOM"
P="pass1234"

curl -s -X POST $BASE/api/register -H "Content-Type: application/json" \
  -d "{\"username\":\"$U\",\"password\":\"$P\"}" > /dev/null
LOGIN=$(curl -s -X POST $BASE/api/login -H "Content-Type: application/json" \
  -d "{\"username\":\"$U\",\"password\":\"$P\"}")
TOKEN=$(echo "$LOGIN" | sed -E 's/.*"token":"([^"]+)".*/\1/')
USER_ID=$(echo "$LOGIN" | sed -E 's/.*"user_id":([0-9]+).*/\1/')
[ -z "$TOKEN" ] && { echo "登录失败"; exit 1; }

# 把任务转给这个用户，跳过上传流程
mysql -u goapp -pgoapp123 videoscope -e \
  "UPDATE video_tasks SET user_id=$USER_ID WHERE id=$TASK_ID;" 2>/dev/null

echo "=== 提问：$QUESTION ==="
curl -s -X POST $BASE/api/task/$TASK_ID/ask -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"question\":\"$QUESTION\"}" | python3 -c "
import json,sys
d = json.load(sys.stdin)
if d.get('code') != 0:
    print('失败:', d)
    sys.exit(1)
data = d['data']
print('答案:', data['answer'])
print('found:', data['found'])
print('引用片段:', len(data['sources']))
for s in data['sources']:
    print(f\"  [{s['start']:.0f}-{s['end']:.0f}秒] {s['content'][:60]}...\")
"
