package main

import (
	"flag"
	"log"
	"net/http"
	"strings"

	"github.com/brbberry/edgelens/internal/dashboard"
	"github.com/brbberry/edgelens/internal/store"
)

func main() {
	databasePath := flag.String("database", "measurements.db", "path to the SQLite measurements database")
	address := flag.String("address", ":8080", "HTTP listen address")
	flag.Parse()

	database, err := store.Open(*databasePath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer database.Close()

	displayAddress := *address
	if strings.HasPrefix(displayAddress, ":") {
		displayAddress = "localhost" + displayAddress
	}
	log.Printf("dashboard listening on http://%s", displayAddress)
	if err := http.ListenAndServe(*address, dashboard.NewHandler(database)); err != nil {
		log.Fatalf("serve dashboard: %v", err)
	}
}
