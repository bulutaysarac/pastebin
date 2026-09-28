package environments

type App struct {
	Port    string
	BaseURL string // prefix for the share links shown to users
}

func GetApp() App {
	return App{
		Port:    GetEnv("PORT", "8080"),
		BaseURL: GetEnv("BASE_URL", "http://localhost:8080"),
	}
}
