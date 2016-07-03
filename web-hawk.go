package main

import (
	"encoding/json"
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

type Status struct {
	Timestamp string         `json:"Timestamp"`
	Services  []ServiceStats `json:"Services"`
}

type ServiceStats struct {
	Name  string  `json:"Name"`
	Alive bool    `json:"Alive"`
	URL   string  `json:"URL"`
	Msec  float64 `json:"Msec"`
}

var cors *string
var urlCleaner []string

func main() {
	portPtr := addConf("PORT", "8080", "Port to host location service on.")
	urlsPtr := addConf("URLS", "http://localhost:7070/up, http://www.clianz.com/", "Comma seperated URLs list to monitor")
	corsPtr := addConf("CORS", "", "CORS URL to configure.")
	dbAddressPtr := addConf("DB_ADDDRESS", "localhost:28015", "Address of RethinkDB instance")
	dbNamePtr := addConf("DB_NAME", "hawk", "Name of RethinkDB database")
	dbUsernamePtr := addConf("DB_USERNAME", "web-hawk", "Username of RethinkDB user")
	dbPasswordPtr := addConf("DB_PASSWORD", "hawkpassw0rd", "Password of RethinkDB user")
	pollTimePtr := addConf("POLL_TIME", "300", "Time (in seconds) between service status polls. '0' will disable server from polling.")
	urlCleanerPtr := addConf("URL_CLEANERS", "http://, https://, www.", "Part of URL to strip for converting to friendly name.")
	flag.Parse()

	cors = corsPtr
	log.Printf("Setting CORS: %v", *cors)
	urlCleaner = strings.Split(strings.Replace(*urlCleanerPtr, " ", "", -1), ",")

	dbSession, err := r.Connect(r.ConnectOpts{
		Address:  *dbAddressPtr,
		Database: *dbNamePtr,
		Username: *dbUsernamePtr,
		Password: *dbPasswordPtr,
	})
	if err != nil {
		log.Panic("Error connecting to DB.", err)
	}

	log.Printf("Monitoring URLs: %v", *urlsPtr)
	urls := strings.Split(strings.Replace(*urlsPtr, " ", "", -1), ",")
	timeout := time.Duration(1 * time.Second)
	client := http.Client{
		Timeout: timeout,
	}

	// Socker server
	socketServer, err := NewSocketServer(nil)
	if err != nil {
		log.Fatal(err)
	}
	socketServer.On("connection", func(so socketio.Socket) {
		// log.Println("on connection")
		so.Join("updatesChannel")
		// so.On("disconnection", func() {
		// 	log.Println("on disconnect")
		// })
	})
	socketServer.On("error", func(so socketio.Socket, err error) {
		log.Println("error:", err)
	})
	http.Handle("/socket.io/", socketServer)

	// Subscibe to DB and broadcast changes
	broadcastDbChanges(socketServer, dbSession)

	// Poll server to monitor periodically
	pollTime, err := strconv.ParseInt(*pollTimePtr, 10, 64)
	if err != nil || (pollTime > 0 && pollTime < 5) {
		panic("Service poll time invalid. Must be 5 seconds or more in interger value.")
	}
	if pollTime != 0 {
		monitorServiceStatus(dbSession, client, urls, pollTime)
	}

	// Web handler
	http.HandleFunc("/up", func(w http.ResponseWriter, r *http.Request) {
		addCors(w)
		statusResp := fetchServerStatusFromDb(dbSession)
		enc := json.NewEncoder(w)
		enc.Encode(statusResp)
	})
	http.HandleFunc("/history", func(w http.ResponseWriter, r *http.Request) {
		addCors(w)
		statusResp := fetchServerStatusHistoryFromDb(dbSession, pollTime)
		enc := json.NewEncoder(w)
		enc.Encode(statusResp)
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
	addCors(w)
	s.Server.ServeHTTP(w, r)
}

func broadcastDbChanges(server *Server, dbSession *r.Session) {
	// Listen to DB for changes
	res, err := r.DB("test").Table("hawk").Changes().Run(dbSession)
	if err != nil {
		log.Fatalf("Error listening to DB changes: %s", err.Error())
	}
	go func(res *r.Cursor) {
		var value map[string]map[string]interface{}
		for res.Next(&value) {
			newStatus := value["new_val"]
			// log.Printf("DB CHANGE: %v", newStatus)
			if newStatus != nil {
				statusByte, _ := json.Marshal(newStatus)
				statusStr := string(statusByte)
				// log.Printf("Broadcasting: %v", statusStr)
				broadcastStatus(server, statusStr)
			}
		}
	}(res)
}

func monitorServiceStatus(session *r.Session, client http.Client, urls []string, pollTime int64) {
	pushServerStatusToDb(session, fetchServerStatus(client, urls))
	ticker := time.NewTicker(time.Duration(pollTime) * time.Second)
	go func() {
		for t := range ticker.C {
			fmt.Println("Tick at", t)
			pushServerStatusToDb(session, fetchServerStatus(client, urls))
		}
	}()
}

func broadcastStatus(server *Server, statusResp string) {
	server.BroadcastTo("updatesChannel", "updateEvent", statusResp)
}

func pushServerStatusToDb(session *r.Session, status Status) {
	// log.Printf("Inserting: %v", status)
	err := r.DB("test").Table("hawk").Insert(status).Exec(session)
	if err != nil {
		log.Printf("Error writing to DB: %v", err)
	}
}

func fetchServerStatusFromDb(session *r.Session) Status {
	var response Status
	resp, err := r.DB("test").Table("hawk").OrderBy(r.Desc("Timestamp")).Limit(1).Run(session)
	if err != nil {
		log.Printf("Error reading DB: %v", err)
	}
	resp.One(&response)
	return response
}

func fetchServerStatusHistoryFromDb(session *r.Session, pollTime int64) []Status {
	limit := (24 * 60 * 60) / pollTime
	var response []Status
	resp, err := r.DB("test").Table("hawk").OrderBy(r.Desc("Timestamp")).Limit(limit).Run(session)
	if err != nil {
		log.Printf("Error reading DB: %v", err)
	}
	resp.All(&response)
	return response
}

func fetchServerStatus(client http.Client, urls []string) Status {
	queue := make(chan ServiceStats, len(urls))
	for _, eachURL := range urls {
		go func(v string) {
			startTime := time.Now()
			resp, err := client.Head(v)
			if err != nil || resp.StatusCode != 200 {
				log.Printf("Error: %v", v)
				queue <- ServiceStats{Name: getNameFromURL(v), Alive: false, URL: v, Msec: 0}
			} else {
				endTime := time.Since(startTime).Seconds() * 1000
				log.Printf("Success (%.2f ms): %v", endTime, v)
				queue <- ServiceStats{Name: getNameFromURL(v), Alive: true, URL: v, Msec: endTime}
			}
		}(eachURL)
	}
	statusResp := Status{Timestamp: time.Now().Format(time.RFC3339)}
	for i := 0; i < len(urls); i++ {
		select {
		case elem := <-queue:
			statusResp.Services = append(statusResp.Services,
				ServiceStats{Name: elem.Name, Alive: elem.Alive, Msec: elem.Msec, URL: elem.URL})
		}
	}
	close(queue)
	return statusResp
}

func getNameFromURL(url string) string {
	for _, v := range urlCleaner {
		url = strings.Replace(url, v, "", -1)
	}
	return url
}

func addConf(name, defaultVal, desc string) *string {
	optPtr := flag.String(name, defaultVal, desc)
	if os.Getenv(name) != "" {
		*optPtr = os.Getenv(name)
	}
	return optPtr
}

func addCors(w http.ResponseWriter) {
	if len(*cors) > 0 {
		w.Header().Set("Access-Control-Allow-Origin", *cors)
		w.Header().Add("Access-Control-Allow-Credentials", "true")
	}
}
