package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	type serviceStats struct {
		Alive bool
		URL   string
		Time  float64
	}
	portPtr := flag.String("port", "8080", "Port to host location service on.")
	// urlsPtr := flag.String("urls", "http://localhost:8080/up,http://www.clianz.com/", "Comma seperated URLs list to monitor")
	urlsPtr := flag.String("urls", "http://localhost:8080/up", "Comma seperated URLs list to monitor")
	flag.Parse()
	if os.Getenv("PORT") != "" {
		*portPtr = os.Getenv("PORT")
	}
	if os.Getenv("URLS") != "" {
		*urlsPtr = os.Getenv("URLS")
	}

	log.Printf("Monitoring URLs: %v", *urlsPtr)
	urls := strings.Split(*urlsPtr, ",")
	timeout := time.Duration(1 * time.Second)
	client := http.Client{
		Timeout: timeout,
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		queue := make(chan serviceStats, len(urls))
		for _, eachURL := range urls {
			go func(v string) {
				startTime := time.Now()
				log.Printf("Parsing URL: %v", v)
				resp, err := client.Head(v)
				if err != nil || resp.StatusCode != 200 {
					log.Printf("Service Error")
					queue <- serviceStats{Alive: false, URL: v, Time: 0}
				} else {
					endTime := time.Since(startTime).Seconds() * 1000
					log.Printf("Success. Duration: %v", endTime)
					queue <- serviceStats{Alive: true, URL: v, Time: endTime}
				}
			}(eachURL)
		}

		fmt.Fprintf(w, "{")
		// for elem := range queue {
		for i := 0; i < len(urls); i++ {
			select {
			case elem := <-queue:
				log.Printf("Alive: %v, Time: %f, URL: %v", elem.Alive, elem.Time, elem.URL)
				if i != 0 {
					fmt.Fprintf(w, ", ")
				}
				fmt.Fprintf(w, "alive: %v, time: %v, url: \"%v\"", elem.Alive, elem.Time, elem.URL)
			}
		}
		close(queue)
		fmt.Fprintf(w, "}")
	})
	err := http.ListenAndServe(":"+*portPtr, nil)
	if err != nil {
		log.Fatalf("Error: %s", err.Error())
	}
	log.Printf("Server running on port %v", *portPtr)
}
