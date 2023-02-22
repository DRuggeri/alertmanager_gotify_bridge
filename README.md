# Alertmanager to Gotify webhook bridge

An [Alertmanager](https://prometheus.io/docs/alerting/latest/alertmanager/)-aware webhook endpoint that converts messages to [Gotify](https://gotify.net/) API calls.

Typically, this should have been created as a [Gotify Plugin](https://gotify.net/docs/plugin)... but after much trial and error, the plugin system never could build properly. I also prefer the idea of separating the translation layer from needing to be built on the *exact* version of the running Gotify server.

## Installation

### Binaries

Download the already existing [binaries](https://github.com/DRuggeri/alertmanager_gotify_bridge/releases) for your platform:

```bash
$ ./alertmanager_gotify_bridge <flags>
```

### From source

Using the standard `go install` (you must have [Go](https://golang.org/) already installed in your local machine):

```bash
$ go install github.com/DRuggeri/alertmanager_gotify_bridge
$ alertmanager_gotify_bridge <flags>
```

## Usage
NOTE: All parameters may be set as environment entries as well as provided on the command line. The environment entry is the same as the flag but converted to all capital letters.

For example, the environment entry for `gotify_token` may be set as `GOTIFY_TOKEN`

### Flags

```
usage: alertmanager_gotify_bridge [<flags>]

Flags:
  --help                        Show context-sensitive help (also try --help-long and --help-man).
  --gotify_endpoint="http://127.0.0.1:80/message"
                                Full path to the Gotify message endpoint ($GOTIFY_ENDPOINT)
  --bind_address=0.0.0.0        The address the bridge will listen on ($BIND_ADDRESS)
  --port=8080                   The port the bridge will listen on ($PORT)
  --webhook_path="/gotify_webhook"
                                The URL path to handle requests on ($WEBHOOK_PATH)
  --timeout=5s                  The number of seconds to wait when connecting to gotify ($TIMEOUT)
  --title_annotation="summary"  Annotation holding the title of the alert ($TITLE_ANNOTATION)
  --message_annotation="description"
                                Annotation holding the alert message ($MESSAGE_ANNOTATION)
  --priority_annotation="priority"
                                Annotation holding the priority of the alert ($PRIORITY_ANNOTATION)
  --default_priority=5          Annotation holding the priority of the alert ($DEFAULT_PRIORITY)
  --metrics_auth_username=METRICS_AUTH_USERNAME
                                Username for metrics interface basic auth ($AUTH_USERNAME and $AUTH_PASSWORD)
  --metrics_namespace="alertmanager_gotify_bridge"
                                Metrics Namespace ($METRICS_NAMESPACE)
  --metrics_path="/metrics"     Path under which to expose metrics for the bridge ($METRICS_PATH)
  --extended_details            When enabled, alerts are presented in HTML format and include colorized status (FIR|RES), alert start time, and a link to the generator of the alert ($EXTENDED_DETAILS)
  --dispatch_errors             When enabled, alerts will be tried to dispatch with a error-message regarding faulty templating or missing fields to help debugging ($DISPATCH_ERRORS)
  --debug                       Enable debug output of the server
  --version                     Show application version.
```

### Token Override
By default, the bridge sends alerts to the initialized bridge Gotify token. This configuration allows all alerts from alertmanager to send to a single Gotify application based on the token.

The bridge supports overriding the initialized bridge Gotify token by setting the `token` query string parameter, which allows different receivers to send alerts to other applications in Gotify.

CURL Example:
```shell
curl http://127.0.0.1:8080/gotify_webhook?token=GS46-fGs.gW-gE. -d '
{ "alerts": [
  {
    "annotations": {
      "description":"A description",
      "summary":"A summary",
      "priority":"critical"
    },
    "status": "firing",
    "generatorURL": "http://foobar.com"
  }
]}
'
```
YAML Example:
```YAML
receivers:
- name: storage
  webhook_configs:
  - url: http://127.0.0.1:8080/gotify_webhook?token=GS46-fGs.gW-gE.
    send_resolved: false
```

### Templating
The supports [Go templating](https://golang.org/pkg/text/template/) with [Prometheus-enhanced functions](https://prometheus.io/docs/prometheus/latest/configuration/template_reference/), so you can customize the alert messages further with templates in the title and message annotations.

For example add following line to the title:  
`{{if eq .Status "firing"}}🔥{{else}}✅{{end}}`  
This differentiates firing from resolving alerts.  
  
Also, there are two methods you can use for additional customisation:
```
.Values                 Access alert-values, -labels and -metrics. 
                        Returns list of:
                            Metric  string
                            Labels  map[string]string
                            Value   float64
              
.Humanize <float64>     Rounds float and stripps trailing zeros to return more readable float.
                        .Humanize 5.3234134 returns 5.32
                        .Humanize 5.0       returns 5
```
To give further information and examples for use-cases for these methods:
Imagine a simple uptime-metric for multiple instances or jobs. If you configure an alert, it would fire if any instance or alert is down. The message would probably say something like "an instance or job is down".
But from the message you would not know which of the jobs or instances is the down one, or if there are multiple. To address this you have to use the `.Values` method. A alert-description could look like this:
```
{{if eq .Status "firing"}}
Following Providers are down: 
{{range $i, $provider := .Values}}
{{$provider.Labels.job}}, 
{{end}}
{{else}}
All providers are back up
{{end}}
```
Now if the alert fires it would list the jobs that are down. Which information the `.Values` method contains can be inspected in the Grafana alertmanager when configuring an alert and clicking the `Preview Alert` button.

### Template Functions
The bridges Go templating supports several template functions. All template functions listed in the [Grafana template functions](https://grafana.com/docs/grafana/latest/alerting/fundamentals/annotation-label/template-functions/) are supported with the bridge, with usage examples.

NOTE: The externalURL function will only return a result when the message is sent from Grafana. Messages initiated through alertmanager will not contain an externalURL.

The bridge uses Prometheous's [template functions](https://prometheus.io/docs/prometheus/latest/configuration/template_reference/). Some of the template functions in [template.go](https://github.com/prometheus/prometheus/blob/main/template/template.go) are not supported in the bridge because of limitations. The chart below lists the additional functions not found in the [Grafana template functions](https://grafana.com/docs/grafana/latest/alerting/fundamentals/annotation-label/template-functions/) documentation, but can be called through the bridge.

| Function Name   | Supported |
| --------------- |:---------:|
| query           | no        |
| first           | no        |
| label           | no        |
| value           | no        |
| strvalue        | no        |
| safeHtml        | yes       |
| sortByLabel     | no        |
| stripPort       | yes       |
| stripDomain     | yes       |
| toTime          | yes       |
| parseDuration   | yes       |

Grafana Example:
```go
{{ reReplaceAll ".+\\|" " " .Labels.log }}
```

CURL Example1:
```json
curl http://127.0.0.1:8080/gotify_webhook -d '
{ "alerts": [
  {
    "annotations": {
      "description": "{{ match \"my+\" \"my|test\" }}",
      "summary":"A summary",
      "priority":"critical"
    },
    "status": "firing",
    "generatorURL": "http://foobar.com"
  }
]}
'
```

CURL Example2:
```json
curl http://127.0.0.1:8080/gotify_webhook -d '
{ "alerts": [
  {
    "annotations": {
      "description": "{{ reReplaceAll \"fir\" \" \" .Status }}",
      "summary":"A summary",
      "priority":"critical"
    },
    "status": "firing",
    "generatorURL": "http://foobar.com"
  }
]}
'
```
### Bridge Message Templating
The bridge now supports user-defined templating for all inbound messages. The user-defined templating can be used for the title and/or message. Also, user-defined templating overrides the default title and message annotations. All keys and values in the JSON from alertmanager can be used in the user-defined template. Any failures in the templates will result in the bridge defaulting back to default alerting.

#### Usage Notes:
- For Docker, you must bind your volume to your host to add user-defined templating.
- The default directory for all templates is the root of the bridge in the folder called `templates`.
- User-defined templating allows matching and linking using the "define" name of the template [Go Templating](https://golang.google.cn/pkg/text/template/).
- The Gotify software token is used for matching a message template.
- The Gotify software token with `title=` at the beginning is used for matching a title template.
- All file names must be unique but can be any name or subfolder naming you choose.
- Only one Gotify software token should be defined. More than one will result in inconsistent alerting results.
- The bridge supports the following template file extensions: "gohtml", "gotmpl", and "tmpl".
- Add `-` to prevent extra blank lines. Example: `{{- .Status }}`

#### Usage Hints:
- When different alert senders/JSON files are sent to the same Gotify software token, we recommend using a minimum of two different user-defined templates to handle the message. For example, `message_template.tmpl` and `message_sub-template1.tmpl`. The `message_template.tmpl` would contain the defined Gotify software token with comparison operators on the JSON values to direct the message to a user-defined sender template. Multiple sub-templates can link to the `message_template.tmpl`. Following this process can limit unexpected execution failures, that result in the alert defaulting back to the default alert.
- Additional folders are supported within the `templates` directory for organization purposes.
- When referencing a key in a JSON that does not exist, the template will fail. The trick is to use the `message_template.tmpl` to direct the message to another template that contains all the required keys. As long as no match occurs in the `message_template.tmpl` to another sub-template that has non-matching keys, everything will continue to alert by design. 

#### Usage Example:
This example will show the creation of five different user-defined templates for more advanced match routing within the bridge. The example below will display a message being received and how to create a custom template based on that message.

JSON Discover Hint: Enable debugging on the bridge to see the JSON output sent from the sender to add additional values to your template.

1. Write down the Gotify software token used in the alert message.
    ```text
    GS46-fGs.gW-gE.
    ```
2. Find the incoming JSON for the user-defined alert.
    ```JSON
    { "alerts": [
      {
        "annotations": {
          "description": "{{ humanize 1234567.0 }}",
          "summary":"A summary",
          "priority":"critical"
        },
        "status": "firing",
        "generatorURL": "http://foobar.com",
      }
    ]}
    ```
3. Create a file in the ./templates folder called `app_message_template.tmpl`.
4. Past the following into the file from step 3.
    ```go
    {{ define "GS46-fGs.gW-gE." }}
    {{ if eq (.Status) ("firing") }} {{ template "my_app1" .}} {{end}}
    {{ if eq (.Annotations.summary) ("A summary") }} {{ template "my_app2" .}} {{end}}
    {{ if eq (.GeneratorURL) ("http://foobar.com") }} {{ template "my_app3" .}} {{else}}Testing Message: None Found {{end}}
    {{end}}
    ```
4. Create a file in the ./templates folder called `app1_message_sub-template.tmpl`.
5. Past the following into the file from step 4.
    ```go
    {{ define "my_app1" }}
    {{ if eq (.Status) ("firing") }}App1 Firing {{else}}App1 Not Firing {{end}}
    {{end}}
    ```
6. Create a file in the ./templates folder called `app2_message_sub-template.tmpl`.
7. Past the following into the file from step 5.
    ```go
    {{ define "my_app2" }}
    {{ if eq (.Status) ("firing") }}App2 Firing {{else}}App2 Not Firing {{end}}
    {{end}}
    ```
8. Create a file in the ./templates folder called `app3_message_sub-template.tmpl`.
9. Past the following into the file from step 8.
    ```go
    {{ define "my_app3" }}
    {{ if eq (.Status) ("firing") }}App3 Firing {{else}}App3 Not Firing {{end}}
    {{end}}
    ```
10. Create a file in the ./templates folder called `app_title_template.tmpl`.
9. Past the following into the file from step 8.
    ```go
    {{ define "title=AFRy-fWs.jW-gE." }}
    {{ if eq (.Status) ("firing") }}Title Sample {{else}}Nope {{end}}
    {{end}}
    ```
12. Send the alert to the bridge, and the result will be listed below in text format.
    - Note: This example is a simple example using the same incoming JSON, but this can support different JSON being sent through the same token, but shows you can get creative with your templates.
    Result:
    ```text
    Title Sample


    App1 Firing 
    
    
    App2 Firing 
    
    
    App3 Firing 
    ```

## Metrics
The bridge tracks telemetry data for metrics within the server as well as exposes gotify's health (obtained via the /health endpoint) as prometheus metrics. Therefore, the bridge can be scraped with Prometheus on /metrics to obtain these metrics.

Exported metrics:
- alertmanager_gotify_bridge_requests_received: Number of HTTP requests received regardless of being wel-formed
- alertmanager_gotify_bridge_requests_invalid: Number of HTTP requests received that were apparently invalid HTTP requests
- alertmanager_gotify_bridge_alerts_received: Overall number of alerts that were received, regardless of being well-formed
- alertmanager_gotify_bridge_alerts_invalid: Number of alerts that were missing required fields and could not be dispatched to gotify
- alertmanager_gotify_bridge_alerts_processed: Number of alerts that were succesfully translated and dispatched to gotify
- alertmanager_gotify_bridge_alerts_failed: Number of alerts that could not be sent to gotify after decoding
- alertmanager_gotify_bridge_gotify_up: Simple up/down for whether the /health endpoint could be probed by the bridge
- alertmanager_gotify_bridge_gotify_health_health: Whether the /health endpoint returns "green" for "health"
- alertmanager_gotify_bridge_gotify_health_database: Whether the /health endpoint returns "green" for "database"

## Docker
An official scratch-based Docker image is built with every tag and pushed to DockerHub and ghcr. Additionally, PRs will be tested by GitHubs actions.

The following images are available for use:
- [druggeri/alertmanager_gotify_bridge](https://hub.docker.com/r/druggeri/alertmanager_gotify_bridge)
- [ghcr.io/DRuggeri/alertmanager_gotify_bridge](https://ghcr.io/DRuggeri/alertmanager_gotify_bridge)

### Docker-Compose
```
  alertmanager_gotify_bridge:
    image: ghcr.io/druggeri/alertmanager_gotify_bridge:master
    container_name: alertmanager_gotify_bridge
    environment:
      - GOTIFY_TOKEN=xxxxxxx
      - GOTIFY_ENDPOINT=http://gotify:80/message
    ports:
      - 8080:8080
    restart: unless-stopped
```
Supported tags:
- master (state of master branch)
- latest (latest tag or master)
- vX.Y.Z (eg. v0.6.0, specific version)
- vX.Y (latest maj/minor version)

## Community Contributions
* A docker container of this bridge is also maintained by [ndragon798](https://github.com/ndragon798) on [docker hub](https://hub.docker.com/r/nathaneaston/alertmanager_gotify_bridge-docker)

## Testing and Development
If you would like to experiment and test your bridge configuration, you can simulate Prometheus alerts like so

Start the bridge with your choice of parameters. For example: set a bogus token, enable debug, and listen on port 8080 while accepting all other defaults:
`
GOTIFY_TOKEN=FOO ./alertmanager_gotify_bridge --port=8080 --debug
`

You can then send a request with cURL to see if your configuration works out as expected:
```
curl http://127.0.0.1:8080/gotify_webhook -d '
{ "alerts": [
  {
    "annotations": {
      "description":"A description",
      "summary":"A summary",
      "priority":"critical"
    },
    "status": "firing",
    "generatorURL": "http://foobar.com"
  }
]}
'
```
