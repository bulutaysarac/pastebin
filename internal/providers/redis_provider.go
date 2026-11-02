package providers

import (
	"context"
	"fmt"
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
	redis.SetLogger(redisLogger{})

	addr := environments.GetRedis().Addr
	rdb := redis.NewClient(&redis.Options{
		Addr:                  addr,
		DialTimeout:           redisTimeout,
		ReadTimeout:           redisTimeout,
		WriteTimeout:          redisTimeout,
		PoolTimeout:           redisTimeout,
		ContextTimeoutEnabled: true,
		// The callers fall back at once instead. By default it would dial five
		// times and take a second to find Redis down.
		MaxRetries:    -1,
		DialerRetries: 1,
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

// redisLogger keeps the client's own lines out of the log at normal levels: it
// would print one per failed dial, and the callers already log what failed.
type redisLogger struct{}

func (redisLogger) Printf(_ context.Context, format string, v ...any) {
	slog.Debug("redis: " + fmt.Sprintf(format, v...))
}
