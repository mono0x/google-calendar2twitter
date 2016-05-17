package main

import (
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"google.golang.org/api/calendar/v3"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/ChimeraCoder/anaconda"
	"github.com/joho/godotenv"
)

const (
	MaxStatuses = 200
	MaxEvents   = 100
)

func main() {
	log.SetFlags(log.Lshortfile)

	if len(os.Args) > 0 {
		if err := godotenv.Load(os.Args...); err != nil {
			log.Fatal(err)
		}
	}

	anaconda.SetConsumerKey(os.Getenv("TWITTER_CONSUMER_KEY"))
	anaconda.SetConsumerSecret(os.Getenv("TWITTER_CONSUMER_SECRET"))

	api := anaconda.NewTwitterApi(os.Getenv("TWITTER_OAUTH_TOKEN"), os.Getenv("TWITTER_OAUTH_TOKEN_SECRET"))
	defer api.Close()

	calendarId := os.Getenv("CALENDAR_ID")
	if calendarId == "" {
		log.Fatal("CALENDAR_ID is required.")
	}
	template := os.Getenv("TEMPLATE")
	if template == "" {
		log.Fatal("TEMPLATE is required.")
	}

	userId := strings.SplitN(os.Getenv("TWITTER_OAUTH_TOKEN"), "-", 2)[0]
	if _, err := strconv.ParseInt(userId, 10, 64); err != nil {
		log.Fatal(err)
	}

	timeZone := os.Getenv("TIMEZONE")

	recentUrls := make(map[string]struct{})
	{
		v := url.Values{}
		v.Set("user_id", userId)
		v.Set("count", strconv.Itoa(MaxStatuses))
		timeline, err := api.GetUserTimeline(v)
		if err != nil {
			log.Fatal(err)
		}

		for _, status := range timeline {
			for _, url := range status.Entities.Urls {
				recentUrls[url.Expanded_url] = struct{}{}
			}
		}
	}

	var events *calendar.Events
	{
		json, err := ioutil.ReadFile("google_client_credentials.json")
		if err != nil {
			log.Fatal(err)
		}

		config, err := google.JWTConfigFromJSON(json, calendar.CalendarReadonlyScope)
		if err != nil {
			log.Fatal(err)
		}

		client := config.Client(oauth2.NoContext)

		service, err := calendar.New(client)
		if err != nil {
			log.Fatal(err)
		}

		updatedMin := time.Now().AddDate(0, 0, -1).Format(time.RFC3339)

		events, err = service.Events.List(calendarId).UpdatedMin(updatedMin).MaxResults(MaxEvents).SingleEvents(true).Do()
		if err != nil {
			log.Fatal(err)
		}
	}

	now := time.Now()

events:
	for _, event := range events.Items {
		if event.Status == "cancelled" {
			continue events
		}
		link := event.HtmlLink
		if timeZone != "" {
			link += "&ctz=" + timeZone
		}

		if _, ok := recentUrls[link]; ok {
			continue
		}
		recentUrls[link] = struct{}{}

		var date string
		if event.Start.Date != "" {
			startLoc, err := time.LoadLocation(event.Start.TimeZone)
			if err != nil {
				log.Fatal(err)
			}
			start, err := time.ParseInLocation("2006-01-02", event.Start.Date, startLoc)
			if err != nil {
				log.Fatal(err)
			}
			if now.After(start) {
				continue events
			}
			date = start.Format("01/02")
		} else if event.Start.DateTime != "" {
			startLoc, err := time.LoadLocation(event.Start.TimeZone)
			if err != nil {
				log.Fatal(err)
			}
			start, err := time.ParseInLocation(time.RFC3339, event.Start.DateTime, startLoc)
			if err != nil {
				log.Fatal(err)
			}
			if now.After(start) {
				continue events
			}
			date = start.Format("01/02")
		}

		var location string
		{
			parts := strings.SplitN(event.Location, ",", 2)
			location = parts[0]
		}

		replacer := strings.NewReplacer(
			"{title}", event.Summary,
			"{url}", link,
			"{date}", date,
			"{location}", location,
		)
		text := replacer.Replace(template)
		_, err := api.PostTweet(text, url.Values{})
		if err != nil {
			if apiErr, ok := err.(*anaconda.ApiError); ok {
				for _, err := range apiErr.Decoded.Errors {
					if err.Code == anaconda.TwitterErrorStatusIsADuplicate {
						continue events
					}
				}
				log.Fatal(apiErr)
			} else {
				log.Fatal(err)
			}
		}
	}
}
