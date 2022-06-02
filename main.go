package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"regexp"
	"strconv"
	s "strings"
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
	if err != nil {
		log.Fatalf("Error loading .env file")
	}

	beanUrl := "https://www.ranchogordo.com/collections/out-of-stock-beans"

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	res, err := mainRequest(beanUrl, client)
	assertErrorToNilf("unable to reach website: %v", err)

	defer res.Body.Close()

	// scrape phase
	todayBeans, err := scraper(res)
	assertErrorToNilf("scrape unsuccessful: %v", err)

	// determine key names
	yesterdayKey, todayKey, err := key()
	assertErrorToNilf("key creation unsuccessful: %v", err)

	// pull yesterday's data from s3
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

	log.Println("Yesterday:", yesterdayBeans)

	// compare yesterday's waitlist with today's
	available := []string{}

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

	if err != nil {
		log.Printf("failed to upload today's scraping data: %v", err)
	} else {
		log.Println("today's scraping data successfully uploaded")
	}

	// email results
	if len(available) == 0 {
		message := "Subject: No new beans\r\n" + "\r\n" + "No beans have been removed from the waitlist\r\n"
		err := text(message, false)
		assertErrorToNilf("text attempt unsuccessful: %v", err)
	} else {
		textUrls := checkURL(available)
		beansAndUrls := append(available, textUrls...)
		availBeans := s.Join(beansAndUrls, ", ")
		err := text(availBeans, true)
		assertErrorToNilf("text attempt unsuccessful: %v", err)
	}
}

func mainRequest(url string, client *http.Client) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to create request: %v", err)
	}

	uAgent := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/100.0.4896.88 Safari/537.36"

	req.Header.Set("User-Agent", uAgent)

	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %v", err)
	}

	return res, nil
}

func scraper(res *http.Response) (Beans, error) {
	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return nil, fmt.Errorf("error parsing website: %v", err)
	}

	todayBeans := Beans{}

	doc.Find("div.sold-out").Each(func(i int, s *goquery.Selection) {
		name := s.Find("p.grid-link__title").First().Text()
		if name == "" {
			message := []byte("issue with scrape -- please check selectors")
			email(message)
		}
		todayBeans[name] = true
	})

	return todayBeans, nil
}

func text(beans string, available bool) error {
	client := twilio.NewRestClient()
	params := &openapi.CreateMessageParams{}
	params.SetTo(os.Getenv("TO_PHONE_NUMBER"))
	params.SetFrom(os.Getenv("TWILIO_PHONE_NUMBER"))
	if available {
		availBeans := beans
		params.SetBody(fmt.Sprintf(`The following beans are now available: 
		%s
		`, availBeans))
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

func checkURL(available []string) []string {
	base := "https://www.ranchogordo.com/products/"
	textUrls := []string{}
	for _, v := range available {
		body, err := quickRequest(base, v)
		if err != nil {
			log.Printf("unable to check URL for %q", v)
		}
		wrongURL := regexp.MustCompile("404-not-found").MatchString(body)
		if wrongURL {
			textUrls = append(textUrls, "https://www.ranchogordo.com/")
		} else {
			textUrls = append(textUrls, base+v)
		}
	}
	return textUrls
}

func quickRequest(url, name string) (string, error) {
	res, err := http.Get(url + name)
	if err != nil {
		return "", fmt.Errorf("checkURL failing: %v", err)
	}

	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("error with checkURL ReadAll: %v", err)
	}
	return string(body), nil
}

func key() (string, string, error) {
	t := time.Now()
	today := int(t.Weekday())
	var yesterday int
	if today == 0 {
		yesterday = 6
	} else {
		yesterday = int(t.Weekday() - 1)
	}
	yesterdayKey := s.Join([]string{"waitlistedBeans", strconv.Itoa(yesterday)}, "")
	todayKey := s.Join([]string{"waitlistedBeans", strconv.Itoa(today)}, "")
	if yesterdayKey == "" || todayKey == "" {
		return yesterdayKey, todayKey, errors.New("problem with key creation")
	}
	return yesterdayKey, todayKey, nil
}

func email(message []byte) {
	from := os.Getenv("FROM")
	password := os.Getenv("PASSWORD")

	to := []string{
		os.Getenv("TO"),
	}
	smtpHost := os.Getenv("SMTP_HOST")
	smtpPort := os.Getenv("SMTP_PORT")

	body := message

	auth := smtp.PlainAuth("", from, password, smtpHost)

	addr := s.Join([]string{smtpHost, smtpPort}, ":")

	emailErr := smtp.SendMail(addr, auth, from, to, body)
	if emailErr != nil {
		log.Println("email failed to send", emailErr)
		return
	}
	log.Printf("Email successfully sent to %s", to[0])
}

func assertErrorToNilf(msg string, err error) {
	if err != nil {
		log.Fatalf(msg, err)
	}
}