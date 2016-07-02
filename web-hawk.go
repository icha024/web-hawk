package main

import (
	"flag"
	"fmt"
	"github.com/googollee/go-socket.io"
	r "gopkg.in/dancannon/gorethink.v2"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type serviceStats struct {
	Alive bool
	URL   string
	Time  float64
}

var cors *string
var server *Server

func main() {
	portPtr := addConf("PORT", "8080", "Port to host location service on.")
	urlsPtr := addConf("URLS", "http://localhost:7070/up,http://www.clianz.com/", "Comma seperated URLs list to monitor")
	corsPtr := addConf("CORS", "*", "CORS URL to configure.")
	dbAddressPtr := addConf("DB_ADDDRESS", "localhost:28015", "Address of RethinkDB instance")
	dbNamePtr := addConf("DB_NAME", "hawk", "Name of RethinkDB database")
	dbUsernamePtr := addConf("DB_USERNAME", "web-hawk", "Username of RethinkDB user")
	dbPasswordPtr := addConf("DB_PASSWORD", "hawkpassw0rd", "Password of RethinkDB user")
	pollTimePtr := addConf("POLL_TIME", "10", "Time (in seconds) between service status polls")
	flag.Parse()

	cors = corsPtr
	log.Printf("Setting CORS: %v", *cors)

	session, err := r.Connect(r.ConnectOpts{
		Address:  *dbAddressPtr,
		Database: *dbNamePtr,
		Username: *dbUsernamePtr,
		Password: *dbPasswordPtr,
	})
	if err != nil {
		log.Panic("Error connecting to DB.", err)
	}

	log.Printf("Monitoring URLs: %v", *urlsPtr)
	urls := strings.Split(*urlsPtr, ",")
	timeout := time.Duration(1 * time.Second)
	client := http.Client{
		Timeout: timeout,
	}

	// Socker server
	server, err = NewSocketServer(nil)
	if err != nil {
		log.Fatal(err)
	}
	server.On("connection", func(so socketio.Socket) {
		log.Println("on connection")
		so.Join("updatesChannel")
		so.On("disconnection", func() {
			log.Println("on disconnect")
		})
	})
	server.On("error", func(so socketio.Socket, err error) {
		log.Println("error:", err)
	})
	http.Handle("/socket.io/", server)

	// Poll server to monitor periodically
	latestStatus := fetchServerStatus(client, urls)
	broadcastStatus(latestStatus)
	pushServerStatusToDb(session, latestStatus)
	pollTime, err := strconv.ParseInt(*pollTimePtr, 10, 64)
	if err != nil {
		panic("Service poll time invalid. Must be interger")
	}
	ticker := time.NewTicker(time.Duration(pollTime) * time.Second)
	go func() {
		for t := range ticker.C {
			fmt.Println("Tick at", t)
			latestStatus := fetchServerStatus(client, urls)
			broadcastStatus(latestStatus)
			pushServerStatusToDb(session, latestStatus)
		}
	}()

	// Web handler
	http.HandleFunc("/up", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", *cors)
		statusResp := fetchServerStatusFromDb(session)
		fmt.Fprintf(w, statusResp)
	})
	err = http.ListenAndServe(":"+*portPtr, nil)
	if err != nil {
		log.Fatalf("Error: %s", err.Error())
	}
	log.Printf("Server running on port %v", *portPtr)
}

// Server container for socker server
type Server struct {
	socketio.Server
}

// NewSocketServer to add CORS, see: https://github.com/googollee/go-socket.io/issues/122
func NewSocketServer(transportNames []string) (*Server, error) {
	ret, err := socketio.NewServer(transportNames)
	if err != nil {
		return nil, err
	}
	return &Server{*ret}, nil
}

func (s Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Access-Control-Allow-Origin", *cors)
	w.Header().Add("Access-Control-Allow-Credentials", "true")
	s.Server.ServeHTTP(w, r)
}

func broadcastStatus(statusResp string) {
	server.BroadcastTo("updatesChannel", "updateEvent", statusResp)
}

func pushServerStatusToDb(session *r.Session, status string) {
	err := r.DB("test").Table("hawk").Insert(map[string]string{
		"status":    status,
		"timestamp": time.Now().Format(time.RFC3339),
	}).Exec(session)
	if err != nil {
		log.Printf("Error writing to DB: %v", err)
	}
}

func fetchServerStatusFromDb(session *r.Session) string {
	var response map[string]interface{}
	resp, err := r.DB("test").Table("hawk").OrderBy(r.Desc("timestamp")).Limit(1).Run(session)
	if err != nil {
		log.Printf("Error reading DB: %v", err)
	}
	resp.One(&response)
	return response["status"].(string)
}

func fetchServerStatus(client http.Client, urls []string) string {
	queue := make(chan serviceStats, len(urls))
	for _, eachURL := range urls {
		go func(v string) {
			startTime := time.Now()
			resp, err := client.Head(v)
			if err != nil || resp.StatusCode != 200 {
				log.Printf("Error: %v", v)
				queue <- serviceStats{Alive: false, URL: v, Time: 0}
			} else {
				endTime := time.Since(startTime).Seconds() * 1000
				log.Printf("Success (%.2f ms): %v", endTime, v)
				queue <- serviceStats{Alive: true, URL: v, Time: endTime}
			}
		}(eachURL)
	}

	statusResp := "{\"services\":["
	for i := 0; i < len(urls); i++ {
		select {
		case elem := <-queue:
			if i != 0 {
				statusResp += ","
			}
			statusResp += fmt.Sprintf("{\"alive\":%v,\"msec\":%.2f,\"url\":\"%v\"}", elem.Alive, elem.Time, elem.URL)
		}
	}
	close(queue)
	statusResp += "]}"
	return statusResp
}

func addConf(name, defaultVal, desc string) *string {
	optPtr := flag.String(name, defaultVal, desc)
	if os.Getenv(name) != "" {
		*optPtr = os.Getenv(name)
	}
	return optPtr
}
