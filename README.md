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
  --help                       Show context-sensitive help (also try --help-long and --help-man).
  --gotify_endpoint="http://127.0.0.1:80/message"
                               Full path to the Gotify messages endpoint
  --bind_address=0.0.0.0       The address the bridge will listen on
  --port=8080                  The port the bridge will listen on
  --webhook_path="/gotify_webhook"
                               The URL path to handle requests on
  --timeout=5s                 The number of seconds to wait when connecting to gotify
  --title_annotation="description"
                               Annotation holding the title of the alert
  --message_annotation="summary"
                               Annotation holding the alert message
  --priority_annotation="priority"
                               Annotation holding the priority of the alert
  --default_priority=5         Annotation holding the priority of the alert
  --debug                      Enable debug output of the server
  --version                    Show application version.
  --details=0                  Amount of details in messages. 0 = default, 1 = extended
```
## Community Contributions
* A docker container of this bridge is maintained by [ndragon798](https://github.com/ndragon798) on [docker hub](https://hub.docker.com/r/nathaneaston/alertmanager_gotify_bridge-docker)
