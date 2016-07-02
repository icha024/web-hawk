package main

import (
	"flag"
	"fmt"
	// r "gopkg.in/dancannon/gorethink.v2"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type serviceStats struct {
	Alive bool
	URL   string
	Time  float64
}

func main() {
	portPtr := addConf("PORT", "8080", "Port to host location service on.")
	urlsPtr := addConf("URLS", "http://localhost:7070/up,http://www.clianz.com/", "Comma seperated URLs list to monitor")
	corsPtr := addConf("CORS", "*", "CORS URL to configure.")
	flag.Parse()

	// session, err := r.Connect(r.ConnectOpts{
	// 	Address:  "localhost:28015",
	// 	Database: "hawk",
	// 	Username: "web-hawk",
	// 	Password: "hawkpassw0rd",
	// })

	log.Printf("Monitoring URLs: %v", *urlsPtr)
	urls := strings.Split(*urlsPtr, ",")
	timeout := time.Duration(1 * time.Second)
	client := http.Client{
		Timeout: timeout,
	}

	http.HandleFunc("/up", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", *corsPtr)
		statusResp := fetchServerStatus(client, urls)
		fmt.Fprintf(w, statusResp)
	})
	err := http.ListenAndServe(":"+*portPtr, nil)
	if err != nil {
		log.Fatalf("Error: %s", err.Error())
	}
	log.Printf("Server running on port %v", *portPtr)
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
