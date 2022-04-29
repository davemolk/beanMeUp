package main

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/smtp"
	"os"
	s "strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/joho/godotenv"
)

type Beans map[string]bool

const beanUrl = "https://www.ranchogordo.com/collections/out-of-stock-beans"

func main() {
	uAgent := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/100.0.4896.88 Safari/537.36"

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
	defer res.Body.Close()

	if res.StatusCode != 200 {
		log.Fatalf("status code error: %d %s", res.StatusCode, res.Status)
	}

	// scrape phase
	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		log.Fatal("error parsing Body", err)
	}

	today := Beans{}

	doc.Find("div.sold-out").Each(func(i int, s *goquery.Selection) {
		name := s.Find("p.grid-link__title").First().Text()
		if name == "" {
			message := []byte("issue with scrape -- please check selectors")
			email(message)
		}
		today[name] = true
	})
	
	// pull yesterday's data from s3
	sess := session.Must(session.NewSession(&aws.Config{
		Region: aws.String("us-west-1"),
	}))

	s3Client := s3.New(sess)

	requestInput := &s3.GetObjectInput{
		Bucket: aws.String("beanwaitlist"),
		Key:    aws.String("waitlistedBeans"),
	}

	result, err := s3Client.GetObject(requestInput)
	if err != nil {
		log.Fatal("unable to get s3 data\n", err)
	}

	defer result.Body.Close()
	b, err := ioutil.ReadAll(result.Body)
	if err != nil {
		log.Fatal("error reading body\n", err)
	}

	yesterday := Beans{}
	if err := json.Unmarshal(b, &yesterday); err != nil {
		log.Fatal("error unmarshalling data", err)
	}

	fmt.Println("Yesterday:", yesterday)
	
	// compare (currently dummy data)
	available := []string{}

	for name := range yesterday {
		if _, ok := today[name]; !ok {
			available = append(available, name)
		}
	}

	fmt.Println(available)

	// convert to json for upload
	js, err := json.MarshalIndent(today, "", "\t")
	if err != nil {
		log.Fatal("error with marshalling data", err)
	}

	// figure out system to use for key names
	uploader := s3manager.NewUploader(sess)
	_, ierr := uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String("beanwaitlist"),
		Key: aws.String("waitlistedBeansThursday"),
		Body: bytes.NewReader(js),
	}) 

	if ierr != nil {
		log.Println("failed to upload file\n", ierr.Error())
	} else {
		log.Println("successfully uploaded")
	}
	
	// email results
	buf := &bytes.Buffer{}
	gob.NewEncoder(buf).Encode(available)
	message := buf.Bytes()
	email(message)
	
}

func email(message []byte) {
	err := godotenv.Load(".env")
	if err != nil {
		log.Fatal("Error loading .env", err)
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

	emailErr := smtp.SendMail(addr, auth, from,to, body)
	if emailErr != nil {
		log.Println("unable to send email", emailErr)
		return
	}
	log.Printf("Email sent to %s", to[0])
}