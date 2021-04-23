package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/keybase/go-keybase-chat-bot/kbchat"
	"github.com/prometheus/alertmanager/notify/webhook"
	atmpl "github.com/prometheus/alertmanager/template"
)

var watchdogCache map[string]watchdog = map[string]watchdog{}

type watchdog struct {
	id        string
	lastPing  time.Time
	lastAlert atmpl.Alert
	firing    bool
}

// DefaultFuncs is the default list additional Go Template functions supported.
var DefaultFuncs = template.FuncMap{
	"toUpper": strings.ToUpper,
	"toLower": strings.ToLower,
	"title":   strings.Title,
	// join is equal to strings.Join but inverts the argument order
	// for easier pipelining in templates.
	"join": func(sep string, s []string) string {
		return strings.Join(s, sep)
	},
	"match": regexp.MatchString,
	"reReplaceAll": func(pattern, repl, text string) string {
		re := regexp.MustCompile(pattern)
		return re.ReplaceAllString(text, repl)
	},
	"stringSlice": func(s ...string) []string {
		return s
	},
}

func sendToKeybase(kbc *kbchat.API, recipient string, message string) {
	tlfName := fmt.Sprintf("%s,%s", kbc.GetUsername(), recipient)
	log.Printf("tlfName: %s", tlfName)

	if _, err := kbc.SendMessageByTlfName(tlfName, message); err != nil {
		log.Printf("Error sending message: %+v", err)
	}
}

func handleWebhook(kbc *kbchat.API, recipient string, tmpl *template.Template) func(w http.ResponseWriter, r *http.Request) {

	return func(w http.ResponseWriter, r *http.Request) {
		buf, err := ioutil.ReadAll(r.Body)
		if err != nil {
			log.Printf("Error reading webhook post: %+v", err)
		}

		wh := &webhook.Message{}
		err = json.Unmarshal(buf, wh)
		if err != nil {
			log.Printf("Error parsing webhook post: %+v", err)
		}

		log.Printf("Received and parsed incoming webook: %+v", wh)

		writer := bytes.NewBufferString("")

		tmpl.ExecuteTemplate(writer, "keybaseAlert", *wh)
		log.Printf("%s", writer.String())

		sendToKeybase(kbc, recipient, writer.String())
	}

}

func handleWatchdog(kbc *kbchat.API, recipient string, tmpl *template.Template) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		buf, err := ioutil.ReadAll(r.Body)
		if err != nil {
			log.Printf("Error reading watchdog post: %+v", err)
		}

		wh := &webhook.Message{}
		err = json.Unmarshal(buf, wh)
		if err != nil {
			log.Printf("Error parsing watchdog post: %+v", err)
		}

		log.Printf("Received and parsed incoming watchdog: %+v", wh)

		alerts := wh.Alerts.Firing()
		for _, alert := range alerts {

			hash := sha256.New()
			for _, k := range alert.Labels.SortedPairs().Names() {
				v := alert.Labels[k]
				hash.Write([]byte(fmt.Sprintf("%s:%s", k, v)))
			}

			watchdogID := hex.EncodeToString(hash.Sum([]byte{}))

			log.Printf("Incoming watchdog request for: %+v ID: %s", alert.Labels, watchdogID)

			entry, ok := watchdogCache[watchdogID]

			if ok {

				if entry.firing {
					// Recover the watchdog alert as we've seen pings return

					writer := bytes.NewBufferString("")
					tmpl.ExecuteTemplate(writer, "watchdogAlertRecover", entry.lastAlert)

					sendToKeybase(kbc, recipient, writer.String())
				}

				entry.firing = false
				entry.lastPing = time.Now()
				watchdogCache[watchdogID] = entry

			} else {
				watchdogCache[watchdogID] = watchdog{
					id:        watchdogID,
					lastPing:  time.Now(),
					lastAlert: alert,
					firing:    false,
				}
			}
		}

	}
}

func main() {
	var kbLoc string
	var kbc *kbchat.API
	var listenPort int
	var interval time.Duration
	var expiry time.Duration
	var recipient string
	var templatePath string
	var err error

	flag.StringVar(&kbLoc, "keybase", "keybase", "the location of the Keybase app")
	flag.IntVar(&listenPort, "port", 3000, "Port to listen for webhooks")
	flag.StringVar(&recipient, "recipient", "", "Keybase user or team#channel to send message to")
	flag.DurationVar(&interval, "interval", 10*time.Second, "The interval at which to check for watchdog expiry")
	flag.DurationVar(&expiry, "expiry", 2*time.Minute, "The amount of time after which a non-pinging watchdog check will be considered to have expired")
	flag.StringVar(&templatePath, "template", "default.tmpl", "Go text template definition file")
	flag.Parse()

	tmpl, err := template.New("default.tmpl").Funcs(DefaultFuncs).ParseFiles(templatePath)

	if err != nil {
		log.Panicf("Unable to parse template: %+v", err)
	}
	log.Printf("Templates parsed successfully.")

	if kbc, err = kbchat.Start(kbchat.RunOptions{
		KeybaseLocation:    kbLoc,
		StartService:       true,
		DisableBotLiteMode: true,
		Oneshot: &kbchat.OneshotOptions{
			PaperKey: os.Getenv("KEYBASE_PAPERKEY"),
			Username: os.Getenv("KEYBASE_USERNAME"),
		},
	}); err != nil {
		log.Fatalf("Error creating API: %+v", err)
	}
	log.Printf("Keybase API setup complete.")

	// Start the watchdog timer
	ticker := time.NewTicker(interval)
	go func() {

		for {
			select {
			case _ = <-ticker.C:
				for id, watchdog := range watchdogCache {
					if watchdog.firing == false && time.Now().Sub(watchdog.lastPing) > expiry {
						// Watchdog has expired, we need to alert

						watchdog.firing = true
						watchdogCache[id] = watchdog

						writer := bytes.NewBufferString("")
						tmpl.ExecuteTemplate(writer, "watchdogAlertFire", watchdog.lastAlert)

						sendToKeybase(kbc, recipient, writer.String())
					}
				}
			}
		}

	}()
	log.Printf("Started watchdog timer routine.")

	http.HandleFunc("/webhook", handleWebhook(kbc, recipient, tmpl))
	http.HandleFunc("/watchdog", handleWatchdog(kbc, recipient, tmpl))

	log.Printf("Listening on port %d", listenPort)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", listenPort), nil))

}
