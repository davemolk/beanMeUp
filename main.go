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

const beanUrl = "https://www.ranchogordo.com/collections/out-of-stock-beans"
const uAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/100.0.4896.88 Safari/537.36"

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
	res := makeRequest()
	
	defer res.Body.Close()

	// scrape phase
	todayBeans := scraper(res)

	// determine key names
	yesterdayKey, todayKey, err := key()
	if err != nil {
		log.Fatal("problem with key creation")
	}
	
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
	if err != nil {
		log.Fatal("unable to get s3 data\n", err)
	}

	defer result.Body.Close()
	b, err := io.ReadAll(result.Body)
	if err != nil {
		log.Fatal("error reading body\n", err)
	}

	yesterdayBeans := Beans{}
	if err := json.Unmarshal(b, &yesterdayBeans); err != nil {
		log.Fatal("error unmarshalling data", err)
	}
	
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
	if err != nil {
		log.Fatal("error with marshalling data", err)
	}

	// upload file to s3
	uploader := s3manager.NewUploader(sess)
	_, ierr := uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String("beanwaitlist"),
		Key: aws.String(todayKey),
		Body: bytes.NewReader(js),
	}) 

	if ierr != nil {
		log.Println("failed to upload today's scraping data\n", ierr)
	} else {
		log.Println("today's scraping data successfully uploaded")
	}

	// check URLS
	results := checkURL(available)
	
	// email results
	if len(available) == 0 {
		message := []byte(
			"Subject: No new beans\r\n" + "\r\n" + "No beans have been removed from the waitlist\r\n",
		)
		email(message)
	} else {
		availBeans := s.Join(available, ", ")
		text(availBeans)
	}
}

func makeRequest() *http.Response {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequest("GET", beanUrl, nil)
	if err != nil {
		log.Fatal("unable to set request", err)
	}
	req.Header.Set("User-Agent", uAgent)

	res, err := client.Do(req)
	if err != nil {
		log.Fatal("request failed", err)
	}

	if res.StatusCode != 200 {
		log.Fatalf("status code error: %d %s", res.StatusCode, res.Status)
	}

	return res
}

func scraper(res *http.Response) Beans {
	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		log.Fatal("error parsing Body", err)
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

	return todayBeans
}

func text(beans string) {
	availBeans := beans
	client := twilio.NewRestClient()
	params := &openapi.CreateMessageParams{}
    params.SetTo(os.Getenv("TO_PHONE_NUMBER"))
    params.SetFrom(os.Getenv("TWILIO_PHONE_NUMBER"))
    params.SetBody(fmt.Sprintf(`The following beans are now available: 
	%s
	Find them at: https://www.ranchogordo.com/
	`, availBeans))

	_, err := client.ApiV2010.CreateMessage(params)
    if err != nil {
        log.Println(err.Error())
    } else {
        log.Println("SMS sent successfully!")
    }
}

func checkURL(available []string) []string {
	base1 := "https://www.ranchogordo.com/collections/heirloom-beans/products/"
	base2 := "https://www.ranchogordo.com/collections/the-rancho-gordo-xoxoc-project/products/"
	for _, v := range available {
		// keep a list of what is in which category? more efficent than doing two calls? or, we're opnly
		// talking about 1-2 bean possibilities, so could even do a goroutine situation (if len(available > 1))
		
	}
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
    err := godotenv.Load(".env")
	if err != nil {
		log.Fatalf("Error loading .env file")
	}

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