package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"
	"github.com/prometheus/prometheus/template"
	"gopkg.in/alecthomas/kingpin.v2"
)

var Version = "testing"

type bridge struct {
	server             *http.Server
	debug              *bool
	timeout            *time.Duration
	titleAnnotation    *string
	messageAnnotation  *string
	priorityAnnotation *string
	defaultPriority    *int
	gotifyToken        *string
	gotifyEndpoint     *string
	dispatchErrors     *bool
}

type Notification struct {
	Alerts []Alert
}
type Alert struct {
	Annotations  map[string]string
	Status       string
	Labels       map[string]string
	GeneratorURL string
	StartsAt     string
	ValueString  string
	ExternalURL  string
}

type GotifyNotification struct {
	Title    string                 `json:"title"`
	Message  string                 `json:"message"`
	Priority int                    `json:"priority"`
	Extras   map[string]interface{} `json:"extras"`
}

var (
	gotifyEndpoint = kingpin.Flag("gotify_endpoint", "Full path to the Gotify message endpoint ($GOTIFY_ENDPOINT)").Default("http://127.0.0.1:80/message").Envar("GOTIFY_ENDPOINT").String()

	address     = kingpin.Flag("bind_address", "The address the bridge will listen on ($BIND_ADDRESS)").Default("0.0.0.0").Envar("BIND_ADDRESS").IP()
	port        = kingpin.Flag("port", "The port the bridge will listen on ($PORT)").Default("8080").Envar("PORT").Int()
	webhookPath = kingpin.Flag("webhook_path", "The URL path to handle requests on ($WEBHOOK_PATH)").Default("/gotify_webhook").Envar("WEBHOOK_PATH").String()
	timeout     = kingpin.Flag("timeout", "The number of seconds to wait when connecting to gotify ($TIMEOUT)").Default("5s").Envar("TIMEOUT").Duration()

	titleAnnotation    = kingpin.Flag("title_annotation", "Annotation holding the title of the alert ($TITLE_ANNOTATION)").Default("summary").Envar("TITLE_ANNOTATION").String()
	messageAnnotation  = kingpin.Flag("message_annotation", "Annotation holding the alert message ($MESSAGE_ANNOTATION)").Default("description").Envar("MESSAGE_ANNOTATION").String()
	priorityAnnotation = kingpin.Flag("priority_annotation", "Annotation holding the priority of the alert ($PRIORITY_ANNOTATION)").Default("priority").Envar("PRIORITY_ANNOTATION").String()
	defaultPriority    = kingpin.Flag("default_priority", "Annotation holding the priority of the alert ($DEFAULT_PRIORITY)").Default("5").Envar("DEFAULT_PRIORITY").Int()

	authUsername     = kingpin.Flag("metrics_auth_username", "Username for metrics interface basic auth ($AUTH_USERNAME and $AUTH_PASSWORD)").Envar("AUTH_USERNAME").String()
	authPassword     = ""
	metricsNamespace = kingpin.Flag("metrics_namespace", "Metrics Namespace ($METRICS_NAMESPACE)").Envar("METRICS_NAMESPACE").Default("alertmanager_gotify_bridge").String()
	metricsPath      = kingpin.Flag("metrics_path", "Path under which to expose metrics for the bridge ($METRICS_PATH)").Envar("METRICS_PATH").Default("/metrics").String()
	extendedDetails  = kingpin.Flag("extended_details", "When enabled, alerts are presented in HTML format and include colorized status (FIR|RES), alert start time, and a link to the generator of the alert ($EXTENDED_DETAILS)").Default("false").Envar("EXTENDED_DETAILS").Bool()
	dispatchErrors   = kingpin.Flag("dispatch_errors", "When enabled, alerts will be tried to dispatch with a error-message regarding faulty templating or missing fields to help debugging ($DISPATCH_ERRORS)").Default("false").Envar("DISPATCH_ERRORS").Bool()

	debug   = kingpin.Flag("debug", "Enable debug output of the server").Bool()
	metrics = make(map[string]int)
)

func init() {
	prometheus.MustRegister(version.NewCollector(*metricsNamespace))
}

type basicAuthHandler struct {
	handler  http.HandlerFunc
	username string
	password string
}

type metricsHandler struct {
	svr *bridge
}

func (h *basicAuthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	username, password, ok := r.BasicAuth()
	if !ok || username != h.username || password != h.password {
		log.Printf("Invalid HTTP auth from `%s`", r.RemoteAddr)
		w.Header().Set("WWW-Authenticate", "Basic realm=\"metrics\"")
		http.Error(w, "Invalid username or password", http.StatusUnauthorized)
		return
	}
	h.handler(w, r)
	return
}

func (h *metricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	collector := NewMetricsCollector(&metrics, h.svr, metricsNamespace)
	registry := prometheus.NewRegistry()
	registry.MustRegister(collector)

	newHandler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	newHandler = promhttp.InstrumentMetricHandler(registry, newHandler)
	newHandler.ServeHTTP(w, r)
	return
}

func basicAuthHandlerBuilder(parentHandler http.Handler) http.Handler {
	if *authUsername != "" && authPassword != "" {
		return &basicAuthHandler{
			handler:  parentHandler.ServeHTTP,
			username: *authUsername,
			password: authPassword,
		}
	}

	return parentHandler
}

func main() {
	kingpin.Version(Version)
	kingpin.Parse()

	metrics["requests_received"] = 0
	metrics["requests_invalid"] = 0
	metrics["alerts_received"] = 0
	metrics["alerts_invalid"] = 0
	metrics["alerts_processed"] = 0
	metrics["alerts_failed"] = 0

	gotifyToken := os.Getenv("GOTIFY_TOKEN")
	gotifyToken = "1"
	if gotifyToken == "" {
		os.Stderr.WriteString("ERROR: The token for Gotify API must be set in the environment variable GOTIFY_TOKEN\n")
		os.Exit(1)
	}

	authPassword = os.Getenv("NUT_EXPORTER_WEB_AUTH_PASSWORD")

	if !strings.HasSuffix(*gotifyEndpoint, "/message") {
		os.Stderr.WriteString(fmt.Sprintf("WARNING: /message not at the end of the gotifyEndpoint parameter (%s). Automatically appending it.\n", *gotifyEndpoint))
		toAdd := "/message"
		if strings.HasSuffix(*gotifyEndpoint, "/") {
			toAdd = "message"
		}
		*gotifyEndpoint += toAdd
		os.Stderr.WriteString(fmt.Sprintf("New gotifyEndpoint: %s\n", *gotifyEndpoint))
	}

	_, err := url.ParseRequestURI(*gotifyEndpoint)
	if err != nil {
		fmt.Printf("Error - invalid gotify endpoint: %s\n", err)
		os.Exit(1)
	}

	serverType := ""
	if *debug {
		serverType = "debug "
	}

	fmt.Printf("Starting %sserver on http://%s:%d%s translating to %s ...\n", serverType, *address, *port, *webhookPath, *gotifyEndpoint)
	svr := &bridge{
		debug:              debug,
		timeout:            timeout,
		titleAnnotation:    titleAnnotation,
		messageAnnotation:  messageAnnotation,
		priorityAnnotation: priorityAnnotation,
		defaultPriority:    defaultPriority,
		gotifyToken:        &gotifyToken,
		gotifyEndpoint:     gotifyEndpoint,
		dispatchErrors:     dispatchErrors,
	}

	serverMux := http.NewServeMux()
	serverMux.HandleFunc(*webhookPath, svr.handleCall)
	serverMux.Handle(*metricsPath, basicAuthHandlerBuilder(&metricsHandler{svr: svr}))

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

func (svr *bridge) handleCall(w http.ResponseWriter, r *http.Request) {
	var notification Notification
	var token string
	var externalURL *url.URL
	text := []string{}
	respCode := http.StatusOK

	metrics["requests_received"]++

	appToken := r.URL.Query().Get("token")
	if appToken != "" {
		if *svr.debug {
			fmt.Printf("Gotify application token (%s) found in request URI - overriding default token: (%s)\n", appToken, *svr.gotifyToken)
		}
		token = appToken
	} else {
		if *svr.debug {
			log.Printf("    request uri (%s) application token prefix (?token=) is missing - falling back to default (%s)\n", r.RequestURI, *svr.gotifyToken)
		}
		token = *svr.gotifyToken
	}

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
			log.Printf("bridge: Unmarshal of request failed: %s\n", err)
			log.Printf("\nBEGIN passed data:\n%s\nEND passed data.", string(b))
			http.Error(w, fmt.Sprintf("%s", err), http.StatusBadRequest)
			metrics["requests_invalid"]++
			return
		}

		if *svr.debug {
			log.Printf("Detected %d alerts\n", len(notification.Alerts))
		}

		for idx, alert := range notification.Alerts {
			extras := make(map[string]interface{})
			proceed := true
			title := ""
			message := ""
			priority := *svr.defaultPriority

			metrics["alerts_received"]++
			if *svr.debug {
				log.Printf("    Alert %d", idx)
			}

			if alert.ExternalURL != "" {
				externalURL, err = url.Parse(alert.ExternalURL)
				if err != nil {
					fmt.Printf("External URL Format Error: %s", err)
				}
			}

			if *extendedDetails {
				// set text to html
				extrasContentType := make(map[string]string)
				extrasContentType["contentType"] = "text/html"
				extras["client::display"] = extrasContentType

				switch alert.Status {
				case "resolved":
					message += "<font style='color: #00b339;' data-mx-color='#00b339'>RESOLVED</font><br/> "
					title += "[RES] "
				case "firing":
					message += "<font style='color: #b31e00;' data-mx-color='#b31e00'>FIRING</font><br/> "
					title += "[FIR] "
				}
			}

			if val, ok := alert.Annotations[*svr.titleAnnotation]; ok {
				templatedTitle, err := renderTemplate(val, alert, externalURL)
				if err != nil {
					proceed = false
					text = []string{err.Error()}
					respCode = http.StatusBadRequest
					if *svr.debug {
						log.Println(err.Error())
					}
					if *svr.dispatchErrors {
						proceed = true
						title = "Alertmanager-Gotify-Bridge Error"
						message = fmt.Sprintf("    Error: %s\n\nAlso check Alertmanager, maybe an alert was raised!\n\nIcomming request:\n%s", err.Error(), b)
					}
				} else {
					title += templatedTitle
				}

				if *svr.debug {
					log.Printf("    title: %s\n", title)
				}
			} else {
				proceed = false
				errMsg := fmt.Sprintf("Missing annotation: %s", *svr.titleAnnotation)
				text = []string{errMsg}
				respCode = http.StatusBadRequest
				if *svr.debug {
					log.Println(errMsg)
				}
				if *svr.dispatchErrors {
					proceed = true
					title = "Alertmanager-Gotify-Bridge Error"
					message = fmt.Sprintf("    Error: %s\n\nAlso check Alertmanager, maybe an alert was raised!\n\nIcomming request:\n%s", errMsg, b)
				}
			}

			if val, ok := alert.Annotations[*svr.messageAnnotation]; ok {
				message, err = renderTemplate(val, alert, externalURL)
				if err != nil {
					proceed = false
					text = []string{err.Error()}
					respCode = http.StatusBadRequest
					if *svr.debug {
						log.Println(err.Error())
					}
					if *svr.dispatchErrors {
						proceed = true
						title = "Alertmanager-Gotify-Bridge Error"
						message = fmt.Sprintf("    Error: %s\n\nAlso check Alertmanager, maybe an alert was raised!\n\nIcomming request:\n%s", err.Error(), b)
					}
				}

				if *svr.debug {
					log.Printf("    message: %s\n", message)
				}
			} else {
				proceed = false
				errMsg := fmt.Sprintf("Missing annotation: %s", *svr.messageAnnotation)
				text = []string{errMsg}
				respCode = http.StatusBadRequest
				if *svr.debug {
					log.Println(errMsg)
				}
				if *svr.dispatchErrors {
					proceed = true
					title = "Alertmanager-Gotify-Bridge Error"
					message = fmt.Sprintf("    Error: %s\n\nAlso check Alertmanager, maybe an alert was raised!\n\nIcomming request:\n%s", errMsg, b)
				}
			}

			if val, ok := alert.Annotations[*svr.priorityAnnotation]; ok {
				tmp, err := strconv.Atoi(val)
				if err == nil {
					priority = tmp
					if *svr.debug {
						log.Printf("    priority: %d\n", priority)
					}
				}
			} else {
				if *svr.debug {
					log.Printf("    priority annotation (%s) missing - falling back to default (%d)\n", *svr.priorityAnnotation, *svr.defaultPriority)
				}
			}

			if *extendedDetails {
				if strings.HasPrefix(alert.GeneratorURL, "http") {
					message += "<br/><a href='" + alert.GeneratorURL + "'>go to source</a>"
					extrasNotification := make(map[string]map[string]string)
					extrasNotification["click"] = make(map[string]string)
					extrasNotification["click"]["url"] = alert.GeneratorURL
					extras["client::notification"] = extrasNotification
				}
				if alert.StartsAt != "" {
					message += "<br/><br/><i><font style='color: #999999;' data-mx-color='#999999'> alert created at: " + alert.StartsAt[:19] + "</font></i><br/>"
				}
			}

			if proceed {
				if *svr.debug {
					log.Printf("    Dispatching to gotify...\n")
				}
				outbound := GotifyNotification{
					Title:    title,
					Message:  message,
					Priority: priority,
					Extras:   extras,
				}
				msg, _ := json.Marshal(outbound)
				if *svr.debug {
					log.Printf("    Outbound: %s\n", string(msg))
				}

				client := http.Client{
					Timeout: *svr.timeout * time.Second,
				}

				request, err := http.NewRequest("POST", *svr.gotifyEndpoint, bytes.NewBuffer(msg))
				if err != nil {
					log.Printf("    Error setting up request: %s", err)
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					metrics["alerts_failed"]++
					return
				}
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("X-Gotify-Key", token)

				resp, err := client.Do(request)
				if err != nil {
					log.Printf("    Error dispatching to Gotify: %s", err)
					respCode = http.StatusInternalServerError
					text = append(text, err.Error())
					metrics["alerts_failed"]++
					continue
				} else {
					defer resp.Body.Close()
					body, _ := ioutil.ReadAll(resp.Body)
					if *svr.debug {
						log.Printf("    Dispatched! Response was %s\n", body)
					}
					if resp.StatusCode != 200 {
						log.Printf("Non-200 response from gotify at %s. Code: %d, Status: %s (enable debug to see body)",
							*svr.gotifyEndpoint, resp.StatusCode, resp.Status)
						respCode = resp.StatusCode
						text = append(text, fmt.Sprintf("Gotify Error: %s", resp.Status))
						metrics["alerts_failed"]++
					} else {
						text = append(text, fmt.Sprintf("Message %d dispatched", idx))
						metrics["alerts_processed"]++
					}
					continue
				}
			} else {
				if *svr.debug {
					log.Printf("    Unable to dispatch!\n")
					respCode = http.StatusBadRequest
					text = []string{"Incomplete request"}
					metrics["alerts_invalid"]++
				}
			}
		}
	} else {
		text = []string{"No content sent"}
		respCode = http.StatusBadRequest
	}

	http.Error(w, strings.Join(text, "\n"), respCode)
	return
}

func renderTemplate(templateString string, data interface{}, externalURL *url.URL) (string, error) {
	var result string
	var err error
	var unsupportedFunc string

	//Excludes unsupported template function calls.
	switch {
	case (strings.Contains(templateString, "{{ query") || strings.Contains(templateString, "{{query")):
		unsupportedFunc = "query"
	case (strings.Contains(templateString, "{{ first") || strings.Contains(templateString, "{{first")):
		unsupportedFunc = "first"
	case (strings.Contains(templateString, "{{ label") || strings.Contains(templateString, "{{label")):
		unsupportedFunc = "label"
	case (strings.Contains(templateString, "{{ value") || strings.Contains(templateString, "{{value")):
		unsupportedFunc = "value"
	case (strings.Contains(templateString, "{{ strvalue") || strings.Contains(templateString, "{{strvalue")):
		unsupportedFunc = "strvalue"
	case (strings.Contains(templateString, "{{ safeHtml") || strings.Contains(templateString, "{{safeHtml")):
		unsupportedFunc = "safeHtml"
	case (strings.Contains(templateString, "{{ sortByLabel") || strings.Contains(templateString, "{{sortByLabel")):
		unsupportedFunc = "sortByLabel"
	default:
		unsupportedFunc = ""
	}

	if unsupportedFunc == "" {
		titleTemplate := template.NewTemplateExpander(context.Background(), templateString, "tmp", data, 0, nil, externalURL, nil)
		result, err = titleTemplate.Expand()
		if err != nil {
			return "", fmt.Errorf("error in Template: %s", err)
		}
		return result, err
	} else {
		return "", fmt.Errorf("error in Template: The bridge does not support the function %s", unsupportedFunc)
	}
}

type AlertValues struct {
	Metric string
	Labels map[string]string
	Value  float64
}

func (a Alert) Values() []AlertValues {
	listRegx := regexp.MustCompile("\\[ ?metric='(.*?)' ?labels=\\{(.*?)\\} ?value=(.*?) ?\\]")
	list := listRegx.FindAllStringSubmatch(a.ValueString, -1)

	var alertValues []AlertValues

	for _, query := range list {
		metric := query[1]
		labelsString := query[2]
		value, err := strconv.ParseFloat(query[3], 32)
		if err != nil {
			value = -1
		}

		labelRegx := regexp.MustCompile("([^=, ]+?)=([^=, ]+)")
		labelsList := labelRegx.FindAllStringSubmatch(labelsString, -1)

		labels := make(map[string]string)

		for _, value := range labelsList {
			labels[value[1]] = value[2]
		}

		alertValues = append(alertValues, AlertValues{Metric: metric, Labels: labels, Value: value})
	}

	return alertValues
}

func (a Alert) Humanize(in float64) string {
	in = math.Round(in*100) / 100
	return humanize.Ftoa(in)
}
