package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/joho/godotenv"
	twilio "github.com/twilio/twilio-go"
	openapi "github.com/twilio/twilio-go/rest/api/v2010"
)

// Beans is used to store the names of the waitlisted beans.
type Beans map[string]bool

type Weekday int

const (
	Sunday Weekday = iota
	Monday
	Tuesday
	Wednesday
	Thursday
	Friday
	Saturday
)

func main() {
	err := godotenv.Load(".env")
	assertErrorToNilf("unable to load env: %v", err)

	beanURL := "https://www.ranchogordo.com/collections/out-of-stock-beans"

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := mainRequest(beanURL, client)
	assertErrorToNilf("unable to reach website: %v", err)

	defer resp.Body.Close()

	todayBeans, err := scraper(resp)
	assertErrorToNilf("scrape unsuccessful: %v", err)

	yesterdayKey, todayKey, err := key()
	assertErrorToNilf("key creation unsuccessful: %v", err)

	// get yesterday's data from S3
	sess := session.Must(session.NewSession(&aws.Config{
		Region: aws.String("us-west-1"),
	}))

	s3Client := s3.New(sess)

	requestInput := &s3.GetObjectInput{
		Bucket: aws.String("beanwaitlist"),
		Key:    aws.String(yesterdayKey),
	}

	result, err := s3Client.GetObject(requestInput)
	assertErrorToNilf("unable to get S3 data: %v", err)

	defer result.Body.Close()

	b, err := io.ReadAll(result.Body)
	assertErrorToNilf("unable to read S3 data: %v", err)

	yesterdayBeans := Beans{}
	err = json.Unmarshal(b, &yesterdayBeans)
	assertErrorToNilf("unable to unmarshal data: %v", err)

	log.Println("yesterday's beans:", yesterdayBeans)

	// compare yesterday's waitlist with today's
	var available []string

	for name := range yesterdayBeans {
		if _, ok := todayBeans[name]; !ok {
			available = append(available, name)
		}
	}

	// convert to json for upload
	js, err := json.MarshalIndent(todayBeans, "", "\t")
	assertErrorToNilf("unable to marshal data: %v", err)

	// upload file to s3
	uploader := s3manager.NewUploader(sess)
	_, err = uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String("beanwaitlist"),
		Key:    aws.String(todayKey),
		Body:   bytes.NewReader(js),
	})
	assertErrorToNilf("failed to upload today's scraping data: %v", err)
	log.Println("today's scraping data successfully uploaded")

	// text results
	if len(available) == 0 {
		message := "No beans have been removed from the waitlist"
		err := text(message, false)
		assertErrorToNilf("text attempt unsuccessful: %v", err)
	} else {
		textUrls := checkURL(available)
		beansAndUrls := append(available, textUrls...)
		availBeans := strings.Join(beansAndUrls, ", ")
		err := text(availBeans, true)
		assertErrorToNilf("text attempt unsuccessful: %v", err)
	}
}

// mainRequest makes a GET request to the Rancho Gordo waitlist page, returning the response and any errors.
func mainRequest(url string, client *http.Client) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to create request: %v", err)
	}

	uAgent := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/100.0.4896.88 Safari/537.36"

	req.Header.Set("User-Agent", uAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %v", err)
	}

	return resp, nil
}

// scraper parses the Rancho Gordo waitlist page, recording the names of all waitlisted beans and any errors.
func scraper(resp *http.Response) (Beans, error) {
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error parsing website: %v", err)
	}

	todayBeans := Beans{}

	doc.Find("div.sold-out").Each(func(i int, s *goquery.Selection) {
		name := s.Find("p.grid-link__title").First().Text()
		if name == "" {
			msg := "issue with Bean Counter -- please check selectors"
			err := text(msg, false)
			assertErrorToNilf("text attempt unsuccessful: %v", err)
		}
		todayBeans[name] = true
	})

	return todayBeans, nil
}

// text uses the Twilio API to send an SMS with a passed-in message, returning any errors.
func text(beans string, available bool) error {
	client := twilio.NewRestClient()
	params := &openapi.CreateMessageParams{}
	params.SetTo(os.Getenv("TO_PHONE_NUMBER"))
	params.SetFrom(os.Getenv("TWILIO_PHONE_NUMBER"))
	if available {
		availBeans := beans
		params.SetBody(fmt.Sprintf("The following beans are now available: %s", availBeans))
	} else {
		params.SetBody(beans)
	}

	_, err := client.ApiV2010.CreateMessage(params)
	if err != nil {
		return fmt.Errorf("unable to send text: %v", err)
	}
	log.Println("SMS sent successfully!")
	return nil
}

// checkURL attempts to create a URL for each newly available bean off the waitlist, defaulting to the main Rancho Gordo URL.
func checkURL(available []string) []string {
	base := "https://www.ranchogordo.com/products/"
	var textUrls []string
	var wg sync.WaitGroup
	for _, bean := range available {
		bean = strings.TrimSpace(bean)
		bean = strings.Replace(bean, " ", "-", -1)
		wg.Add(1)
		go func(b string) {
			body, err := quickRequest(base, b)
			if err != nil {
				log.Printf("unable to check %q url", b)
			}
			wrongURL := strings.Contains(body, "404-not-found")
			if wrongURL {
				textUrls = append(textUrls, "https://www.ranchogordo.com/")
			} else {
				textUrls = append(textUrls, base+b)
			}
			wg.Done()
		}(bean)
	}
	wg.Wait()
	return textUrls
}

// quickRequest returns the HTML string of a given page and any errors.
func quickRequest(url, name string) (string, error) {
	name = strings.Replace(name, " ", "-", -1)
	name = strings.ToLower(name)
	resp, err := http.Get(url + name)
	if err != nil {
		return "", fmt.Errorf("checkURL failing: %v", err)
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error with checkURL ReadAll: %v", err)
	}
	return string(body), nil
}

// key uses the current day to create a new key for the S3 data storage. Waitlist data is kept in S3
// for a week before being overwritten.
func key() (string, string, error) {
	var yesterday int
	t := time.Now()
	today := int(t.Weekday())

	if today == 0 {
		yesterday = 6
	} else {
		yesterday = int(t.Weekday() - 1)
	}
	yesterdayKey := strings.Join([]string{"waitlistedBeans", strconv.Itoa(yesterday)}, "")
	todayKey := strings.Join([]string{"waitlistedBeans", strconv.Itoa(today)}, "")
	if yesterdayKey == "" || todayKey == "" {
		return yesterdayKey, todayKey, errors.New("problem with key creation")
	}
	return yesterdayKey, todayKey, nil
}

// assertErrorToNilf is a simple helper function for error handling.
func assertErrorToNilf(msg string, err error) {
	if err != nil {
		log.Fatalf(msg, err)
	}
}
