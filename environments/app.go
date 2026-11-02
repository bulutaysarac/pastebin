package environments

import "time"

type App struct {
	Port             string
	BaseURL          string        // prefix for the share links shown to users
	CreatesPerMinute int           // pastes one client IP may create a minute; 0 = no limit
	CleanupInterval  time.Duration // how often expired pastes are deleted
	ViewsInterval    time.Duration // how often read counts are written to the database
}

func GetApp() App {
	return App{
		Port:             GetEnv("PORT", "8080"),
		BaseURL:          GetEnv("BASE_URL", "http://localhost:8080"),
		CreatesPerMinute: GetEnvInt("RATE_LIMIT_CREATES_PER_MINUTE", 30, 0),
		CleanupInterval:  GetEnvDuration("CLEANUP_INTERVAL", time.Minute),
		ViewsInterval:    GetEnvDuration("VIEWS_INTERVAL", time.Minute),
	}
}
