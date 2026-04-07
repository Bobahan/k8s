package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
)

func main() {
	dbHost := os.Getenv("PG_HOST")
	dbPort := os.Getenv("PG_PORT")
	dbUser := os.Getenv("PG_USER")
	dbPassword := os.Getenv("PG_PASSWORD")
	dbName := os.Getenv("PG_DB")

	// Master Connection
	dsn_master_conn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable", dbHost, dbPort, dbUser, dbPassword, dbName)

	// Slave Connection
	dsn_slave_conn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable", "pg-cluster-ro", "5432", dbUser, dbPassword, dbName)

	db, err := sql.Open("postgres", dsn_master_conn)
	if err != nil {
		log.Fatal("failed to connect to database:", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatal("failed to ping master database:", err)
	}

	fmt.Println("Success Master PostgreSQL connection")

	readDB, err := sql.Open("postgres", dsn_slave_conn)
	if err != nil {
		log.Fatal("failed to connect to database:", err)
	}
	defer db.Close()

	if err := readDB.Ping(); err != nil {
		log.Fatal("failed to ping slave databases:", err)
	}

	fmt.Println("Success Slave PostgreSQL connection")

	handler := mux.NewRouter()

	handler.HandleFunc("/v1/post", func(w http.ResponseWriter, r *http.Request) {
		podName := os.Getenv("HOSTNAME")

		randomNumber := rand.Intn(1000)

		name := fmt.Sprintf("alex-%d", randomNumber)

		var id int
		err := db.QueryRow(`INSERT INTO test(name) VALUES ($1) RETURNING id`, name).Scan(&id)
		if err != nil {
			panic(err)
		}

		fmt.Printf("A record with ID has been created: %d\n", id)
		msg := fmt.Sprintf("Request arrived in Pod: %s\nRecord ID %d", podName, id)
		fmt.Print(msg)
		w.Write([]byte(msg))
	}).Methods("POST")

	handler.HandleFunc("/v1/post", func(w http.ResponseWriter, r *http.Request) {
		podName := os.Getenv("HOSTNAME")

		type Post struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		}

		rows, err := readDB.Query(`SELECT id, name FROM test`)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var posts []Post
		for rows.Next() {
			var p Post
			if err := rows.Scan(&p.ID, &p.Name); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			posts = append(posts, p)
		}

		var isReplica bool
		readDB.QueryRow("SELECT pg_is_in_recovery()").Scan(&isReplica)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Pod       string `json:"pod"`
			IsReplica bool   `json:"is_replica"`
			Posts     []Post `json:"posts"`
		}{Pod: podName, Posts: posts, IsReplica: isReplica})
	}).Methods("GET")

	handler.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("yes am alive!"))
	}).Methods("GET")

	app := New(handler)

	if err := app.Run(); err != nil {
		log.Fatal("failed to start go app")
	}
}

type App struct {
	server  *http.Server
	handler http.Handler
}

func New(handler http.Handler) *App {
	return &App{
		handler: handler,
	}
}

func (app *App) Run() error {
	app.server = &http.Server{
		Addr:         ":3000",
		Handler:      app.handler,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 5 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Println("Server starting on :3000")

	if err := app.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}

	return nil
}
