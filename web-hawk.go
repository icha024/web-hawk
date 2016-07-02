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
	Name  string
	Alive bool
	URL   string
	Time  float64
}

var cors *string
var urlCleaner []string
var server *Server

func main() {
	portPtr := addConf("PORT", "8080", "Port to host location service on.")
	urlsPtr := addConf("URLS", "http://localhost:7070/up, http://www.clianz.com/", "Comma seperated URLs list to monitor")
	corsPtr := addConf("CORS", "", "CORS URL to configure.")
	dbAddressPtr := addConf("DB_ADDDRESS", "localhost:28015", "Address of RethinkDB instance")
	dbNamePtr := addConf("DB_NAME", "hawk", "Name of RethinkDB database")
	dbUsernamePtr := addConf("DB_USERNAME", "web-hawk", "Username of RethinkDB user")
	dbPasswordPtr := addConf("DB_PASSWORD", "hawkpassw0rd", "Password of RethinkDB user")
	pollTimePtr := addConf("POLL_TIME", "600", "Time (in seconds) between service status polls. '0' will disable server from polling.")
	urlCleanerPtr := addConf("URL_CLEANERS", "http://, https://, www.", "Part of URL to strip for converting to friendly name.")
	flag.Parse()

	cors = corsPtr
	log.Printf("Setting CORS: %v", *cors)
	urlCleaner = strings.Split(strings.Replace(*urlCleanerPtr, " ", "", -1), ",")

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
	urls := strings.Split(strings.Replace(*urlsPtr, " ", "", -1), ",")
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
		// log.Println("on connection")
		so.Join("updatesChannel")
		// so.On("disconnection", func() {
		// 	log.Println("on disconnect")
		// })
	})
	server.On("error", func(so socketio.Socket, err error) {
		log.Println("error:", err)
	})
	http.Handle("/socket.io/", server)

	// Subscibe to DB and broadcast changes
	broadcastDbChanges(session)

	// Poll server to monitor periodically
	pollTime, err := strconv.ParseInt(*pollTimePtr, 10, 64)
	if err != nil || (pollTime > 0 && pollTime < 5) {
		panic("Service poll time invalid. Must be 5 seconds or more in interger value.")
	}
	if pollTime != 0 {
		monitorServiceStatus(session, client, urls, pollTime)
	}

	// Web handler
	http.HandleFunc("/up", func(w http.ResponseWriter, r *http.Request) {
		if len(*cors) > 0 {
			w.Header().Set("Access-Control-Allow-Origin", *cors)
			w.Header().Add("Access-Control-Allow-Credentials", "true")
		}
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
	if len(*cors) > 0 {
		w.Header().Add("Access-Control-Allow-Origin", *cors)
		w.Header().Add("Access-Control-Allow-Credentials", "true")
	}
	s.Server.ServeHTTP(w, r)
}

func broadcastDbChanges(session *r.Session) {
	// Listen to DB for changes
	res, err := r.DB("test").Table("hawk").Changes().Run(session)
	if err != nil {
		log.Fatalf("Error listening to DB changes: %s", err.Error())
	}
	go func(res *r.Cursor) {
		var value map[string]map[string]interface{}
		for res.Next(&value) {
			newStatus := value["new_val"]["status"]
			log.Printf("DB CHANGE: %v", newStatus)
			if newStatus != nil {
				if statusStr, ok := newStatus.(string); ok {
					broadcastStatus(statusStr)
				}
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
				queue <- serviceStats{Name: getNameFromURL(v), Alive: false, URL: v, Time: 0}
			} else {
				endTime := time.Since(startTime).Seconds() * 1000
				log.Printf("Success (%.2f ms): %v", endTime, v)
				queue <- serviceStats{Name: getNameFromURL(v), Alive: true, URL: v, Time: endTime}
			}
		}(eachURL)
	}

	statusResp := "{\"updated\":\"" + time.Now().Format(time.Kitchen) + "\",\"services\":["
	for i := 0; i < len(urls); i++ {
		select {
		case elem := <-queue:
			if i != 0 {
				statusResp += ","
			}
			statusResp += fmt.Sprintf("{\"name\":\"%v\",\"alive\":%v,\"msec\":%.2f,\"url\":\"%v\"}",
				elem.Name, elem.Alive, elem.Time, elem.URL)
		}
	}
	close(queue)
	statusResp += "]}"
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
