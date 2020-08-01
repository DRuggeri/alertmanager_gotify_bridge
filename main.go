package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"gopkg.in/alecthomas/kingpin.v2"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type bridge struct {
	server              *http.Server
	debug               *bool
	timeout             *time.Duration
	title_annotation    *string
	message_annotation  *string
	priority_annotation *string
	default_priority    *int
	gotify_token        *string
	gotify_endpoint     *string
}

type Notification struct {
	Alerts []Alert
}
type Alert struct {
	Annotations map[string]string
}

type GotifyNotification struct {
	Title    string `json:"title"`
	Message  string `json:"message"`
	Priority int    `json:"priority"`
}

var (
	gotify_token    = kingpin.Flag("gotify_token", "Application token provisioned in gotify").Required().Envar("GOTIFY_TOKEN").String()
	gotify_endpoint = kingpin.Flag("gotify_endpoint", "Full path to the Gotify messages endpoint").Default("http://127.0.0.1:80/message").Envar("GOTIFY_ENDPOINT").String()

	address      = kingpin.Flag("bind_address", "The address the bridge will listen on").Default("0.0.0.0").Envar("BIND_ADDRESS").IP()
	port         = kingpin.Flag("port", "The port the bridge will listen on").Default("8080").Envar("PORT").Int()
	webhook_path = kingpin.Flag("webhook_path", "The URL path to handle requests on").Default("/gotify_webhook").Envar("WEBHOOK_PATH").String()
	timeout      = kingpin.Flag("timeout", "The number of seconds to wait when connecting to gotify").Default("5s").Envar("TIMEOUT").Duration()

	title_annotation    = kingpin.Flag("title_annotation", "Annotation holding the title of the alert").Default("description").Envar("TITLE_ANNOTATION").String()
	message_annotation  = kingpin.Flag("message_annotation", "Annotation holding the alert message").Default("summary").Envar("SUMMARY_ANNOTATION").String()
	priority_annotation = kingpin.Flag("priority_annotation", "Annotation holding the priority of the alert").Default("priority").Envar("PRIORITY_ANNOTATION").String()
	default_priority    = kingpin.Flag("default_priority", "Annotation holding the priority of the alert").Default("5").Envar("DEFAULT_PRIORITY").Int()

	debug = kingpin.Flag("debug", "Enable debug output of the server").Bool()
)

func main() {
	kingpin.Version("0.0.1")
	kingpin.Parse()

	_, err := url.ParseRequestURI(*gotify_endpoint)
	if err != nil {
		fmt.Printf("Error - invalid gotify endpoint: %s\n", err)
		os.Exit(1)
	}

	fmt.Printf("Starting server on http://%s:%d%s...\n", *address, *port, *webhook_path)
	svr := &bridge{
		debug:               debug,
		timeout:             timeout,
		title_annotation:    title_annotation,
		message_annotation:  message_annotation,
		priority_annotation: priority_annotation,
		default_priority:    default_priority,
		gotify_token:        gotify_token,
		gotify_endpoint:     gotify_endpoint,
	}

	serverMux := http.NewServeMux()
	serverMux.HandleFunc(*webhook_path, svr.handle_call)

	server := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", *address, *port),
		Handler: serverMux,
	}
	svr.server = server

	err = server.ListenAndServe()
	if nil != err {
		fmt.Printf("Error starting the server: %s", err)
		os.Exit(1)
	}
}

func (svr *bridge) handle_call(w http.ResponseWriter, r *http.Request) {
	var notification Notification
	text := ""

	/* Assume this will never fail */
	b, _ := ioutil.ReadAll(r.Body)

	if *svr.debug {
		log.Printf("bridge: Recieved request: %+v\n", r)
		log.Printf("bridge: Headers:\n")
		for name, headers := range r.Header {
			name = strings.ToLower(name)
			for _, h := range headers {
				log.Printf("bridge:  %v: %v", name, h)
			}
		}
		log.Printf("bridge: BODY: %s\n", string(b))
	}

	/* if data was sent, parse the data */
	if string(b) != "" {
		if *svr.debug {
			log.Printf("bridge: data sent - unmarshalling from JSON: %s\n", string(b))
		}

		err := json.Unmarshal(b, &notification)
		if err != nil {
			/* Failure goes back to the user as a 500. Log data here for
			   debugging (which shouldn't ever fail!) */
			log.Fatalf("bridge: Unmarshal of request failed: %s\n", err)
			log.Fatalf("\nBEGIN passed data:\n%s\nEND passed data.", string(b))
			http.Error(w, fmt.Sprintf("%s", err), http.StatusBadRequest)
			return
		}

		if *svr.debug {
			log.Printf("Detected %d alerts\n", len(notification.Alerts))
		}

		for _, alert := range notification.Alerts {
			proceed := true
			title := ""
			message := ""
			priority := *svr.default_priority

			if val, ok := alert.Annotations[*svr.title_annotation]; ok {
				title = val
				if *svr.debug {
					log.Printf("  title: %s\n", title)
				}
			} else {
				proceed = false
				text = fmt.Sprintf("Missing annotation: %s", *svr.title_annotation)
				if *svr.debug {
					log.Printf("  title annotation (%s) missing\n", *svr.title_annotation)
				}
			}

			if val, ok := alert.Annotations[*svr.message_annotation]; ok {
				message = val
				if *svr.debug {
					log.Printf("  message: %s\n", message)
				}
			} else {
				proceed = false
				text = fmt.Sprintf("Missing annotation: %s", *svr.message_annotation)
				if *svr.debug {
					log.Printf("  message annotation (%s) missing\n", *svr.message_annotation)
				}
			}

			if val, ok := alert.Annotations[*svr.priority_annotation]; ok {
				tmp, err := strconv.Atoi(val)
				if err != nil {
					priority = tmp
					if *svr.debug {
						log.Printf("  priority: %d\n", priority)
					}
				}
			} else {
				if *svr.debug {
					log.Printf("  priority annotation (%s) missing - falling back to default (%d)\n", *svr.priority_annotation, *svr.default_priority)
				}
			}

			if proceed {
				if *svr.debug {
					log.Printf("  Required fields found. Dispatching to gotify...\n")
				}
				outbound := GotifyNotification{
					Title:    title,
					Message:  message,
					Priority: priority,
				}
				msg, _ := json.Marshal(outbound)
				if *svr.debug {
					log.Printf("  Outbound: %s\n", string(msg))
				}

				client := http.Client{
					Timeout: *svr.timeout * time.Second,
				}

				request, err := http.NewRequest("POST", *svr.gotify_endpoint, bytes.NewBuffer(msg))
				if err != nil {
					log.Printf("Error setting up request: %s", err)
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					return
				}
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("X-Gotify-Key", *svr.gotify_token)

				resp, err := client.Do(request)
				if err != nil {
					log.Printf("Error dispatching to Gotify: %s", err)
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					return
				} else {
					defer resp.Body.Close()
					body, _ := ioutil.ReadAll(resp.Body)
					if *svr.debug {
						log.Printf("  Dispatched! Response was %s\n", body)
					}
					http.Error(w, resp.Status, resp.StatusCode)
					return
				}
			}
		}
	} else {
		text = "No content sent"
	}

	http.Error(w, text, http.StatusBadRequest)
	return
}
