package environments

type Redis struct {
	Addr string
}

func GetRedis() Redis {
	return Redis{Addr: GetEnv("REDIS_ADDR", "localhost:6379")}
}
