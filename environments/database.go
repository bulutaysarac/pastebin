package environments

import (
	"net"
	"time"

	"github.com/go-sql-driver/mysql"
)

type Database struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string

	MaxOpenConns    int           // connections to MySQL the app may hold at once
	MaxIdleConns    int           // of those, how many stay open between queries
	ConnMaxLifetime time.Duration // a connection is replaced once it is this old
}

func GetDatabase() Database {
	// Under MySQL's max_connections (151) with room for a second instance.
	// database/sql reads 0 as unlimited, so the minimum is 1.
	maxOpen := GetEnvInt("DB_MAX_OPEN_CONNS", 50, 1)

	return Database{
		Host:     GetEnv("DB_HOST", "localhost"),
		Port:     GetEnv("DB_PORT", "3306"),
		User:     GetEnv("DB_USER", "pastebin"),
		Password: GetEnv("DB_PASSWORD", "pastebin"),
		Name:     GetEnv("DB_NAME", "pastebin"),

		// As many as may be open, so a burst's connections are kept for reuse.
		// database/sql reads 0 as "keep none", so the minimum is 1.
		MaxIdleConns:    GetEnvInt("DB_MAX_IDLE_CONNS", maxOpen, 1),
		MaxOpenConns:    maxOpen,
		ConnMaxLifetime: GetEnvDuration("DB_CONN_MAX_LIFETIME", 5*time.Minute),
	}
}

// DSN builds the connection string the MySQL driver expects.
func (d Database) DSN() string {
	cfg := mysql.NewConfig()
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(d.Host, d.Port)
	cfg.User = d.User
	cfg.Passwd = d.Password
	cfg.DBName = d.Name

	return cfg.FormatDSN()
}
