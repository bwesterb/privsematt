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
	"gopkg.in/gomail.v2"
	"gopkg.in/yaml.v2"

	_ "github.com/jinzhu/gorm/dialects/sqlite"
)

// Globals
var (
	conf   Conf
	db     *gorm.DB
	mailer *gomail.Dialer
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

	// Set up mailer
	mailer = gomail.NewDialer("localhost", 25, "", "")

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
			"Missing or malformed request form field: %s", err), 400)
		return
	}

	record := Record{
		When:   time.Now(),
		Name:   request.Name,
		SurfId: request.SurfId,
		EMail:  request.EMail,
	}

	if err := db.Create(&record).Error; err != nil {
		log.Printf("Failed to store attendance record: %s", err)
	}

	m := gomail.NewMessage()
	m.SetHeader("From", "Privacy Seminar <no-reply@metrics.privacybydesign.foundation>")
	m.SetHeader("To", record.EMail)
	m.SetHeader("Subject", "Your presence at Privacy and Identity")
	m.SetBody("text/plain",
		fmt.Sprintf(("Dear student %s,\n"+
			"\n"+
			"This email confirms that your presence at the course\n"+
			"Privacy and Identity has been registered at: %s\n"+
			"\n"+
			"With best regards,\n"+
			"Koning and Jacobs\n"),
			record.Name, record.When))
	go func(m *gomail.Message) {
		if err := mailer.DialAndSend(m); err != nil {
			log.Printf("Failed to so end e-mail: %s", err)
		}
	}(m)
}
