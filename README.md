# goRGWaitList

create request
scrape today's data
pull yesterday's data from s3
compare
email if necessary
upload today's data to s3


create IAM role for lambda to access s3
AWSLambdaExecute

dependencies
go get -u github.com/aws/aws-sdk-go/...
go get github.com/PuerkitoBio/goquery
go get github.com/joho/godotenv (for local, not for aws)
