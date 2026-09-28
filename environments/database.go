package environments

import (
	"net"

	"github.com/go-sql-driver/mysql"
)

type Database struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
}

func GetDatabase() Database {
	return Database{
		Host:     GetEnv("DB_HOST", "localhost"),
		Port:     GetEnv("DB_PORT", "3306"),
		User:     GetEnv("DB_USER", "pastebin"),
		Password: GetEnv("DB_PASSWORD", "pastebin"),
		Name:     GetEnv("DB_NAME", "pastebin"),
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
