
### Keybase Alertmanager Webhook Notification Handler

This tool accepts [Alertmanager](https://github.com/prometheus/alertmanager) webhook notification HTTP POST requests and forwards them to [Keybase](https://keybase.io). It also offers a Watchdog feature where if notifications are not received at a regular interval, an alert will be fired to Keybase.

#### Building
This project vendors its dependencies and they are maintained using Go modules.
```bash
$ go build -mod=vendor .
```

### Running

```bash
$ ./kbam -recipient <keybase recipient>
```
Additional options are available to tweak behaviour:
```bash
$ ./kbam --help
Usage of ./kbam:
  -expiry duration
        The amount of time after which a non-pinging watchdog check will be considered to have expired (default 2m0s)
  -interval duration
        The interval at which to check for watchdog expiry (default 10s)
  -keybase string
        the location of the Keybase app (default "keybase")
  -port int
        Port to listen for webhooks (default 3000)
  -template string
        Go text template definition file (default "default.tmpl")
  -recipient string
        Keybase user or team#channel to send message to
```

### Templates

In a similar way to Alertmanager itself, keybase-alertmanager offers customisable templating via Go's built-in text templating. See `default.tmpl` for defaults. Note that the Go template file must define templates named: `keybaseAlert`, `watchdogAlertFire` and `watchdogAlertRecover`.

A custom template file can be used by specifying the `-template <file path>` command line option.

 #### Example Alertmanager Configuration
```yaml
global:
  resolve_timeout: 5m
route:
  receiver: keybase
  group_by:
  - job
  routes:
  - receiver: watchdog
    match:
      alertname: Watchdog
    group_wait: 15s
    group_interval: 30s
    repeat_interval: 1m
  repeat_interval: 1h
receivers:
- name: noop-receiver
- name: keybase
  webhook_configs:
  - send_resolved: true
    url: http://keybase-alertmanager.default:3000/webhook
- name: watchdog
  webhook_configs:
  - url: http://keybase-alertmanager.default:3000/watchdog
  ```