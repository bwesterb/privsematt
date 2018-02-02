package main

import (
	"crypto/subtle"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jinzhu/gorm"
	"github.com/rs/cors"
	"gopkg.in/yaml.v2"

	_ "github.com/jinzhu/gorm/dialects/sqlite"
)

// Globals
var (
	conf Conf
	db   *gorm.DB
)

// Configuration
type Conf struct {
	AllowedAuthorizationTokens []string
	BindAddr                   string // address to bind to, eg. ":8080"
	DbPath                     string
}

// Data sent along with the POST request
type Request struct {
	Name   string
	SurfId string
	EMail  string
}

// Records of attendence
type Record struct {
	ID     uint `gorm:"primary_key"`
	When   time.Time
	Name   string
	SurfId string
	EMail  string
}

func main() {
	var confPath string

	// Configuration defaults
	conf.DbPath = "db.sqlite3"

	// parse commandline
	flag.StringVar(&confPath, "config", "config.yaml",
		"Path to configuration file")
	flag.Parse()

	// parse configuration file
	if _, err := os.Stat(confPath); os.IsNotExist(err) {
		fmt.Printf("Could not find config file: %s\n", confPath)
		fmt.Println("It should look like")
		fmt.Println("")
		fmt.Println("   bindaddr: ':9090'")
		os.Exit(1)
		return
	}

	buf, err := ioutil.ReadFile(confPath)
	if err != nil {
		log.Fatalf("Could not read config file %s: %s", confPath, err)
	}

	if err := yaml.Unmarshal(buf, &conf); err != nil {
		log.Fatalf("Could not parse config file: %s", err)
	}

	// connect to database
	log.Println("Opening database ...")
	db, err = gorm.Open("sqlite3", conf.DbPath)

	if err != nil {
		log.Fatalf(" %s: could not open to: %s", conf.DbPath, err)
	}
	defer db.Close()
	log.Println(" ok")

	log.Println("Auto-migration (if necessary) ...")
	db.AutoMigrate(Record{})
	log.Println(" ok")

	// set up HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/", submitHandler)
	corsMiddleWare := cors.New(cors.Options{
		AllowCredentials: true,
		AllowedHeaders:   []string{"Authorization"},
	})

	log.Printf("Listening on %s", conf.BindAddr)

	if len(conf.AllowedAuthorizationTokens) == 0 {
		log.Println("Warning: 'allowedauthorizationtokens' is empty!")
		log.Println("           --- I will accept data from anyone!")
	}

	log.Fatal(http.ListenAndServe(conf.BindAddr, corsMiddleWare.Handler(mux)))
}

// Check if the right authorization header is present
func checkAuthorization(w http.ResponseWriter, r *http.Request) bool {
	if len(conf.AllowedAuthorizationTokens) == 0 {
		return true
	}

	auth := strings.SplitN(r.Header.Get("Authorization"), " ", 2)
	if len(auth) != 2 || auth[0] != "Basic" {
		http.Error(w, "Bad or missing Authorization header", http.StatusBadRequest)
		return false
	}

	token := []byte(auth[1])
	for _, okToken := range conf.AllowedAuthorizationTokens {
		if subtle.ConstantTimeCompare(token, []byte(okToken)) == 1 {
			return true
		}
	}
	http.Error(w, "Access denied", http.StatusUnauthorized)
	return false
}

// Handle /submit HTTP requests used to submit events
func submitHandler(w http.ResponseWriter, r *http.Request) {
	var request Request

	if !checkAuthorization(w, r) {
		return
	}

	err := json.Unmarshal([]byte(r.FormValue("request")), &request)
	if err != nil {
		http.Error(w, fmt.Sprintf(
			"Missing or malformed events form field: %s", err), 400)
		return
	}

	record := Record{
		When:   time.Now(),
		Name:   request.Name,
		SurfId: request.SurfId,
		EMail:  request.EMail,
	}

	db.Create(&record)
}
