package providers

import (
	"context"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/redis/go-redis/v9/maintnotifications"

	"github.com/bulutaysarac/pastebin/environments"
)

// redisTimeout bounds every step of a Redis call. Redis answers in well under
// a millisecond, so this much means it is in trouble, and the app is better
// off without it.
const redisTimeout = 100 * time.Millisecond

// ConnectRedis builds the Redis client. Redis being down doesn't stop the app:
// the client connects when it is first used, and every user of it works
// without Redis.
func ConnectRedis() *redis.Client {
	addr := environments.GetRedis().Addr
	rdb := redis.NewClient(&redis.Options{
		Addr:                  addr,
		DialTimeout:           redisTimeout,
		ReadTimeout:           redisTimeout,
		WriteTimeout:          redisTimeout,
		PoolTimeout:           redisTimeout,
		ContextTimeoutEnabled: true,
		MaxRetries:            -1, // the callers fall back at once instead
		// Both are extra commands on every new connection, of no use here.
		DisableIdentity:          true,
		MaintNotificationsConfig: &maintnotifications.Config{Mode: maintnotifications.ModeDisabled},
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		slog.Warn("redis not reachable, running without it for now", "addr", addr, "err", err)
		return rdb
	}
	slog.Info("redis connected", "addr", addr)
	return rdb
}
