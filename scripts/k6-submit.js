// 提交接口压测：每个迭代提交一个不同的任务，测的是真实的首次提交路径
import http from 'k6/http';
import { check } from 'k6';
import exec from 'k6/execution';

const BASE = 'http://127.0.0.1:8080';
const cfg = JSON.parse(open('/tmp/k6-config.json'));

// 429 是限流器正常工作的结果，不算传输失败。
// 把它纳入预期状态，失败率指标才反映真实的错误。
http.setResponseCallback(http.expectedStatuses(200, 429));

export const options = {
  scenarios: {
    submit: {
      executor: 'shared-iterations',
      vus: 10,
      iterations: cfg.tasks.length,
      maxDuration: '2m',
    },
  },
  thresholds: {
    // 成功提交的延迟验收线
    'http_req_duration{status:200}': ['p(95)<50', 'p(99)<100'],
    // 被限流的请求应该快速返回，不该有额外开销
    'http_req_duration{status:429}': ['p(95)<10'],
    // 排除限流后的失败率
    http_req_failed: ['rate<0.01'],
  },
};

export default function () {
  // 每个迭代分配一个不同的任务，避免所有请求打到同一个已完成的任务上
  const idx = exec.scenario.iterationInTest % cfg.tasks.length;
  const md5 = cfg.tasks[idx];

  const res = http.post(
    `${BASE}/api/analyze`,
    JSON.stringify({ md5 }),
    {
      headers: {
        Authorization: `Bearer ${cfg.token}`,
        'Content-Type': 'application/json',
      },
      tags: { name: 'analyze' },
    },
  );

  check(res, {
    'status 200 or 429': (r) => r.status === 200 || r.status === 429,
  });
}
