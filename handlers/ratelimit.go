package handlers

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	eMid "github.com/labstack/echo/v4/middleware"
	"golang.org/x/time/rate"

	"github.com/bulutaysarac/pastebin/internal/metrics"
)

// createBurst is how many pastes a client may create back to back before the
// per-minute rate applies.
const createBurst = 10

// sharedLimitTimeout bounds a call to the shared limit, so a hung Redis can't
// hold up creating a paste.
const sharedLimitTimeout = 100 * time.Millisecond

// SharedLimit is a create limit shared by all instances of the app.
type SharedLimit interface {
	Allow(ctx context.Context, id string, perMinute, burst int) (bool, error)
}

// createLimitStore asks the shared limit and, when that fails, this instance's
// own one. The limit stays on either way. It only gets looser, by the number of
// instances, until the shared one is back.
type createLimitStore struct {
	shared    SharedLimit
	local     eMid.RateLimiterStore
	perMinute int
	log       *slog.Logger
}

func (s *createLimitStore) Allow(id string) (bool, error) {
	if s.shared == nil {
		return s.local.Allow(id)
	}
	ctx, cancel := context.WithTimeout(context.Background(), sharedLimitTimeout)
	defer cancel()

	ok, err := s.shared.Allow(ctx, id, s.perMinute, createBurst)
	if err != nil {
		metrics.ObserveRateLimitFallback()
		s.log.Warn("shared create limit failed, using this instance's own", "err", err)
		return s.local.Allow(id)
	}
	return ok, nil
}

// LimitCreates lets each client IP create perMinute pastes a minute, after a
// burst of createBurst. 0 turns the limit off. Without a shared limit, each
// instance counts on its own.
func LimitCreates(perMinute int, shared SharedLimit, log *slog.Logger) echo.MiddlewareFunc {
	if perMinute == 0 {
		return func(next echo.HandlerFunc) echo.HandlerFunc { return next }
	}

	// A denied client gets its next paste when one token comes back.
	retryAfter := strconv.Itoa(int(math.Ceil(60 / float64(perMinute))))

	return eMid.RateLimiterWithConfig(eMid.RateLimiterConfig{
		Store: &createLimitStore{
			shared:    shared,
			perMinute: perMinute,
			log:       log,
			local: eMid.NewRateLimiterMemoryStoreWithConfig(eMid.RateLimiterMemoryStoreConfig{
				Rate:  rate.Limit(float64(perMinute) / 60),
				Burst: createBurst,
				// By then an idle client's bucket is full again, so forgetting it
				// changes nothing.
				ExpiresIn: createBurst * time.Minute / time.Duration(perMinute),
			}),
		},
		// Nothing sits in front of the app, so the client is the TCP peer.
		// RealIP() would trust X-Forwarded-For, and any client can set that to
		// get a fresh bucket on every request.
		IdentifierExtractor: func(c echo.Context) (string, error) {
			return echo.ExtractIPDirect()(c.Request()), nil
		},
		DenyHandler: func(c echo.Context, _ string, _ error) error {
			c.Response().Header().Set("Retry-After", retryAfter)
			return c.String(http.StatusTooManyRequests, "too many pastes from your address, try again in "+retryAfter+"s")
		},
	})
}
