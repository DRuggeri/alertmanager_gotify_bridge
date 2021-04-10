package main

import (
	"encoding/json"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"
)

type MetricsCollector struct {
	metrics   *map[string]int
	svr       *bridge
	namespace string
}

func NewMetricsCollector(metrics *map[string]int, svr *bridge, namespace *string) *MetricsCollector {
	return &MetricsCollector{
		metrics:   metrics,
		svr:       svr,
		namespace: *namespace,
	}
}

func (c *MetricsCollector) Collect(ch chan<- prometheus.Metric) {
	for key, value := range *c.metrics {
		varDesc := prometheus.NewDesc(prometheus.BuildFQName(c.namespace, "", key),
			fmt.Sprintf("Alertmanager-Gotify bridge %s metric", key),
			nil, nil,
		)

		ch <- prometheus.MustNewConstMetric(varDesc, prometheus.GaugeValue, float64(value))
	}

	/* Gather gotify health info */

	/* Trim off /message and add /health. Use TrimSuffix instead of ReplaceAll just in case
	   a user has the string /message in the path (via proxies or whatnot) */

	gotifyUpDesc := prometheus.NewDesc(prometheus.BuildFQName(c.namespace, "", "gotify_up"),
		fmt.Sprintf("Base scrape status for Gotify"),
		nil, nil,
	)

	healthEndpoint := fmt.Sprintf("%s%s", strings.TrimSuffix(*c.svr.gotifyEndpoint, "/message"), "/health")
	client := http.Client{
		Timeout: *c.svr.timeout * time.Second,
	}
	resp, err := client.Get(healthEndpoint)

	/* Always set these since they seem to be visible in /health all the time */
	status := map[string]string{"health": "error", "database": "error"}

	if err != nil {
		ch <- prometheus.MustNewConstMetric(gotifyUpDesc, prometheus.GaugeValue, float64(0))
		log.Printf("Error getting health information from gotify: %v", err)
	} else {
		ch <- prometheus.MustNewConstMetric(gotifyUpDesc, prometheus.GaugeValue, float64(1))
		defer resp.Body.Close()
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Printf("Error reading health status from gotify response: %v", err)
		} else {
			err = json.Unmarshal(body, &status)
			if err != nil {
				log.Printf("Invalid JSON returned from gotify: %v", err)
			}
		}
	}

	for key, value := range status {
		varDesc := prometheus.NewDesc(prometheus.BuildFQName(c.namespace, "gotify_health", key),
			fmt.Sprintf("Gotify health metric '%s'", key),
			nil, nil,
		)
		exportedValue := 0
		if value == "green" {
			exportedValue = 1
		}
		ch <- prometheus.MustNewConstMetric(varDesc, prometheus.GaugeValue, float64(exportedValue))
	}
}

func (c *MetricsCollector) Describe(ch chan<- *prometheus.Desc) {
}
