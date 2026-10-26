package handlers

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	eMid "github.com/labstack/echo/v4/middleware"
	"golang.org/x/time/rate"
)

// createBurst is how many pastes a client may create back to back before the
// per-minute rate applies.
const createBurst = 10

// LimitCreates lets each client IP create perMinute pastes a minute, after a
// burst of createBurst. 0 turns the limit off.
func LimitCreates(perMinute int) echo.MiddlewareFunc {
	if perMinute == 0 {
		return func(next echo.HandlerFunc) echo.HandlerFunc { return next }
	}

	// A denied client gets its next paste when one token comes back.
	retryAfter := strconv.Itoa(int(math.Ceil(60 / float64(perMinute))))

	return eMid.RateLimiterWithConfig(eMid.RateLimiterConfig{
		Store: eMid.NewRateLimiterMemoryStoreWithConfig(eMid.RateLimiterMemoryStoreConfig{
			Rate:  rate.Limit(float64(perMinute) / 60),
			Burst: createBurst,
			// By then an idle client's bucket is full again, so forgetting it
			// changes nothing.
			ExpiresIn: createBurst * time.Minute / time.Duration(perMinute),
		}),
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
