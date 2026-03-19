package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/lib/pq"
)

func main() {
	// Try with wrong password
	connStr := "host=127.0.0.1 port=5432 user=invoice_user password=WRONG_PASSWORD dbname=invoice_db sslmode=disable"

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	err = db.Ping()
	if err != nil {
		fmt.Println("Expected error with wrong password:", err)
		return
	}

	fmt.Println("Unexpectedly connected to PostgreSQL with wrong password!")
}
