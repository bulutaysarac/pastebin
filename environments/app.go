package environments

type App struct {
	Port             string
	BaseURL          string // prefix for the share links shown to users
	CreatesPerMinute int    // pastes one client IP may create a minute; 0 = no limit
}

func GetApp() App {
	return App{
		Port:             GetEnv("PORT", "8080"),
		BaseURL:          GetEnv("BASE_URL", "http://localhost:8080"),
		CreatesPerMinute: GetEnvInt("RATE_LIMIT_CREATES_PER_MINUTE", 30),
	}
}
