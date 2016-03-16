package main

import (
	"database/sql"
	_ "github.com/lib/pq"
	"log"
	"os"
	"time"
)

func getEnvOrDie(envName string) string {
	envVal := os.Getenv(envName)
	if envVal == "" {
		log.Fatalln(envName, "environment variable is not specified. Exit")
	}
	return envVal
}

func main() {
	dbConnUrl := getEnvOrDie("DEMO_DB")
	mode := getEnvOrDie("DEMO_MODE")
	db, err := sql.Open("postgres", dbConnUrl)
	if err != nil {
		log.Fatalln("Cannot connect to DB with error:", err)
	}
	defer db.Close()
	if mode == "writer" {
		for {
			_, err := db.Exec("INSERT INTO messages VALUES('Hi team!')")
			if err != nil {
				log.Println("Cannot write to DB:", err)
			} else {
				log.Println("Successfully written")
			}
			time.Sleep(time.Second)
		}
	} else if mode == "reader" {
		for {
			var count int
			rows, err := db.Query("SELECT COUNT(*) FROM messages")
			if err != nil {
				log.Println("Cannot query with error:", err)
				time.sleep(time.Second)
				continue
			}
			if rows.Next() {
				if err := rows.Scan(&count); err != nil {
					log.Println("Cannot read from the database with error:", err)
				} else {
					log.Println("Total number of messages:", count)
				}
			} else {
				log.Fatalln("Impossible situation happened")
			}
			rows.Close()
			time.Sleep(time.Second / 3)
		}
	} else {
		log.Fatalln("Unknown mode:", mode)
	}
}
