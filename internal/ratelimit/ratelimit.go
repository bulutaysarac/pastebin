// Package ratelimit keeps token buckets in Redis, so all instances of the app
// share one limit per client.
package ratelimit

import (
	"context"

	"github.com/redis/go-redis/v9"
)

const keyPrefix = "ratelimit:create:"

// allow takes a token from the bucket if it has one. It is one script because
// reading, refilling, writing and setting the expiry must happen as one step:
// across separate commands, two instances could spend the same token, and a key
// expiring between them would come back without an expiry.
//
// An idle bucket is full again after burst/rate, so it can expire then:
// forgetting it changes nothing. The time is Redis's, so instances whose clocks
// differ still agree.
var allow = redis.NewScript(`
local rate = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])
local time = redis.call('TIME')
local now = tonumber(time[1]) + tonumber(time[2]) / 1000000

local bucket = redis.call('HMGET', KEYS[1], 'tokens', 'at')
local tokens = tonumber(bucket[1])
local at = tonumber(bucket[2])
if tokens == nil or at == nil then
	tokens = burst
else
	tokens = math.min(burst, tokens + math.max(0, now - at) * rate)
end

local allowed = 0
if tokens >= 1 then
	tokens = tokens - 1
	allowed = 1
end

redis.call('HSET', KEYS[1], 'tokens', tostring(tokens), 'at', tostring(now))
redis.call('PEXPIRE', KEYS[1], math.ceil(burst / rate * 1000))
return allowed
`)

type Redis struct {
	rdb *redis.Client
}

func New(rdb *redis.Client) *Redis {
	return &Redis{rdb: rdb}
}

// Allow reports whether id may go ahead under a limit of perMinute, after a
// burst of burst, and uses up its turn if so.
func (r *Redis) Allow(ctx context.Context, id string, perMinute, burst int) (bool, error) {
	n, err := allow.Run(ctx, r.rdb, []string{keyPrefix + id}, float64(perMinute)/60, burst).Int()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}
