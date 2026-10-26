// Write load: authors creating pastes as fast as the server accepts them.
//   k6 run scripts/load/write.js                     # 20 VUs, 30s
//   k6 run -e VUS=100 -e DURATION=1m scripts/load/write.js
// Start the stack with `RATE_LIMIT_CREATES_PER_MINUTE=0 make run`: with the
// create limit on, all but the first 10 creates get a 429.
import http from 'k6/http';
import { check } from 'k6';

const BASE = __ENV.BASE || 'http://localhost:8080';

export const options = {
  vus: Number(__ENV.VUS || 20),
  duration: __ENV.DURATION || '30s',
  summaryTrendStats: ['avg', 'p(50)', 'p(95)', 'p(99)', 'max'],
};

// About 1.3 KB, the primer's average paste size.
const content = 'x'.repeat(1300);

export default function () {
  // redirects: 0 measures the create itself, not the follow-up GET.
  const res = http.post(`${BASE}/paste`, { content, expires: 'never' }, { redirects: 0 });
  check(res, { 'status 303': (r) => r.status === 303 });
}
