// Read load: many readers opening pastes created in setup().
//   k6 run scripts/load/read.js                      # 50 VUs, 30s, 100 random pastes
//   k6 run -e VUS=300 scripts/load/read.js           # more readers than MySQL max_connections
//   k6 run -e HOT=1 scripts/load/read.js             # every reader opens the same paste
// k6 has no browser cache, so every request reaches the server.
import http from 'k6/http';
import { check } from 'k6';

const BASE = __ENV.BASE || 'http://localhost:8080';
const PASTES = __ENV.HOT === '1' ? 1 : Number(__ENV.PASTES || 100);

export const options = {
  vus: Number(__ENV.VUS || 50),
  duration: __ENV.DURATION || '30s',
  summaryTrendStats: ['avg', 'p(50)', 'p(95)', 'p(99)', 'max'],
};

// About 1.3 KB, the primer's average paste size.
const content = 'x'.repeat(1300);

export function setup() {
  const ids = [];
  for (let i = 0; i < PASTES; i++) {
    const res = http.post(`${BASE}/paste`, { content, expires: 'never' }, { redirects: 0 });
    if (res.status !== 303) {
      throw new Error(`setup: POST /paste returned ${res.status}`);
    }
    ids.push(res.headers['Location'].slice(1));
  }
  return ids;
}

export default function (ids) {
  const id = ids[Math.floor(Math.random() * ids.length)];
  const res = http.get(`${BASE}/${id}`);
  check(res, { 'status 200': (r) => r.status === 200 });
}
