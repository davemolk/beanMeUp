# goRGWaitList

create request
scrape today's data
pull yesterday's data from s3
compare
email if necessary
upload today's data to s3


create IAM role for lambda to access s3
AWSLambdaExecute, name it lambda-s3-role (or whatever)

dependencies
go get -u github.com/aws/aws-sdk-go/...
go get github.com/PuerkitoBio/goquery
go get github.com/joho/godotenv (for local, not for aws)


prep for lambda
GOOS=linux go build main.go
zip main.zip main

create lambda from scratch
go runtime
choose existing role (lambda-s3-role)
upload from .zip
change Handler name from hello to main (runtime settings)
set up environment variables in configuration

go to Amazon EventBridge
create a new rule
scheduled
chron:  0 15 * * ? * (or whatever)
choose lambda function as target and pick your lambda function

