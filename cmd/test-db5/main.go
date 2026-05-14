package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"

	"invoice-backend/internal/config"

	_ "github.com/lib/pq"
)

func main() {
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		cfg, err := config.Load()
		if err != nil {
			log.Fatalf("load config: %v", err)
		}
		connStr = databaseURL(cfg.Database)
		fmt.Println("DATABASE_URL is not set; using DATABASE_* config resolved from .env/SSM")
	}

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	err = db.Ping()
	if err != nil {
		fmt.Println("Connection failed:", err)
		return
	}

	fmt.Println("Successfully connected to PostgreSQL!")
}

func databaseURL(cfg config.DatabaseConfig) string {
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.User, cfg.Password),
		Host:   cfg.Host + ":" + strconv.Itoa(cfg.Port),
		Path:   cfg.Name,
	}

	q := u.Query()
	q.Set("sslmode", cfg.SSLMode)
	q.Set("connect_timeout", "10")
	u.RawQuery = q.Encode()

	return u.String()
}
