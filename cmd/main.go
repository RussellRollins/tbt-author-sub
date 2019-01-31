package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	htmp "html/template"
	"os"
	"strings"
	ttmp "text/template"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
	"github.com/gocolly/colly"
	sendgrid "github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
)

const (
	kathrynPLID      = "kathyrn-varn"
	baseURL          = "https://www.tampabay.com"
	storySelector    = ".author-page__story-list div.author-page__story-list--list-item"
	localFileStorage = ".tbt"
)

type summarySender interface {
	sendSummary(to *mail.Email, txtContent string, htmlContent string) error
}

type storyStorer interface {
	saveStories([]*story) error
	filterStories([]*story) ([]*story, error)
}

type subscriber struct {
	local         bool
	summarySender summarySender
	storyStorer   storyStorer
}

type story struct {
	URL      string `selector:"a.feed__item[href]" attr:"href" json:"url"`
	Headline string `selector:"h3.feed__headline" json:"headline"`
	Summary  string `selector:"div.feed__summary" json:"summary"`
}

func (s *story) valid() bool {
	return s.URL != "" && s.Headline != "" && s.Summary != ""
}

func main() {
	local := os.Getenv("TBT_LOCAL") != ""
	if local {
		if err := run(local); err != nil {
			fmt.Printf("error while executing tbt-author-sub: %s", err.Error())
			os.Exit(1)
		}
		os.Exit(0)
	}
	lambda.Start(HandleTrigger)
}

type Trigger struct{}

func HandleTrigger(ctx context.Context, empty Trigger) (string, error) {
	if err := run(false); err != nil {
		return fmt.Sprintf("tbt-author-sub failed with err: %s", err.Error()), err
	}
	return "success!", nil

}

func run(local bool) error {
	sub := subscriber{
		local: local,
	}

	if sub.local {
		fmt.Println("Starting in local mode...")
		sub.summarySender = &localSender{}
		sub.storyStorer = &localStorage{}
	} else {
		sub.summarySender = &sendgridSender{key: os.Getenv("SENDGRID_KEY")}
		sub.storyStorer = &dynamoStorage{svc: dynamodb.New(session.New())}
	}

	stories, err := getStories()
	if err != nil {
		return err
	}

	newStories, err := sub.storyStorer.filterStories(stories)
	if err != nil {
		return err
	}

	if len(newStories) == 0 {
		fmt.Println("Nothing to do!")
		return nil
	}

	txtRes, htmlRes, err := writeEmail(newStories)
	if err != nil {
		return err
	}

	recipients := []*mail.Email{
		mail.NewEmail("Russell Rollins", "russell1124@gmail.com"),
	}

	for _, r := range recipients {
		if err := sub.summarySender.sendSummary(r, txtRes, htmlRes); err != nil {
			return err
		}
	}

	if err := sub.storyStorer.saveStories(newStories); err != nil {
		return err
	}

	return nil
}

func getStories() ([]*story, error) {
	stories := make([]*story, 0)

	c := colly.NewCollector()

	c.OnHTML(storySelector, func(e *colly.HTMLElement) {
		// Grab the story info
		s := &story{}
		e.Unmarshal(s)

		// And do some cleanup, this ain't no API

		// Hopefully drop advertisement here, they don't meet our selectors (for now).
		if !s.valid() {
			return
		}

		// They're relative URLs
		s.URL = fmt.Sprintf("%s%s", baseURL, s.URL)

		// They cram a bunch of extra stuff into the Headline field (shrug).
		headSplits := strings.Split(s.Headline, "\n")
		s.Headline = headSplits[0]

		stories = append(stories, s)
	})

	c.Visit(fmt.Sprintf(
		"%s/writers/?plid=%s",
		baseURL,
		kathrynPLID,
	))

	return stories, nil
}

const (
	htmlTemplate = `
<html>
  <meta charset="utf-8">
  <title>Kathryn's Stories</title>
  <body>
    <div class="main">
      <h1>Kathryn's Stories</h1>
      <br><br>


      <h1>Stories:</h1>
      {{range .}}
        <div>
	  <a href="{{.URL}}">{{.Headline}}</a>
	</div>
	<div>
	  {{.Summary}}
	</div>
      {{end}}
    </div>
  </body>
</html>
`
	txtTemplate = `
Kathryn's Stories

{{range .}}
  {{.URL}}
{{end}}
`
)

func writeEmail(stories []*story) (string, string, error) {
	txtTmpl, err := ttmp.New("text_email").Parse(txtTemplate)
	if err != nil {
		return "", "", err
	}
	htmlTmpl, err := htmp.New("html_email").Parse(htmlTemplate)
	if err != nil {
		return "", "", err
	}

	var (
		txtContent  bytes.Buffer
		htmlContent bytes.Buffer
	)
	if err := txtTmpl.Execute(&txtContent, stories); err != nil {
		return "", "", err
	}
	if err := htmlTmpl.Execute(&htmlContent, stories); err != nil {
		return "", "", err
	}

	return txtContent.String(), htmlContent.String(), nil
}

type sendgridSender struct {
	key string
}

func (ss *sendgridSender) sendSummary(to *mail.Email, txtContent string, htmlContent string) error {
	from := mail.NewEmail("Russell Rollins", "russell@russellrollins.com")
	subject := "Kathryn's Stories"
	message := mail.NewSingleEmail(from, subject, to, txtContent, htmlContent)

	s := sendgrid.NewSendClient(ss.key)
	_, err := s.Send(message)
	return err
}

type localSender struct{}

func (ls *localSender) sendSummary(to *mail.Email, txtContent string, htmlContent string) error {
	fmt.Println("Text Content")
	fmt.Println(txtContent)
	fmt.Println("HTML Content")
	fmt.Println(htmlContent)
	return nil
}

type dynamoStorage struct {
	svc *dynamodb.DynamoDB
}

func (ds *dynamoStorage) saveStories(stories []*story) error {
	for _, s := range stories {
		av, err := dynamodbattribute.MarshalMap(s)
		if err != nil {
			return err
		}
		input := &dynamodb.PutItemInput{
			Item:      av,
			TableName: aws.String("stories"),
		}
		if _, err := ds.svc.PutItem(input); err != nil {
			return err
		}
	}

	return nil
}

func (ds *dynamoStorage) filterStories(stories []*story) ([]*story, error) {
	newStories := []*story{}
	for _, s := range stories {
		result, err := ds.svc.GetItem(&dynamodb.GetItemInput{
			TableName: aws.String("stories"),
			Key: map[string]*dynamodb.AttributeValue{
				"url": {
					S: aws.String(s.URL),
				},
			},
		})
		if err != nil {
			return []*story{}, err
		}

		if len(result.Item) == 0 {
			newStories = append(newStories, s)
		}
	}
	return newStories, nil
}

type localStorage struct {
}

func (ls *localStorage) saveStories(stories []*story) error {
	f, err := os.OpenFile(localFileStorage, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for _, s := range stories {
		if _, err := w.WriteString(fmt.Sprintf("%s\n", s.URL)); err != nil {
			return err
		}
	}
	w.Flush()
	return nil
}

func (ls *localStorage) filterStories(stories []*story) ([]*story, error) {
	f, err := os.OpenFile(localFileStorage, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return []*story{}, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	knownURLs := []string{}
	for scanner.Scan() {
		knownURLs = append(knownURLs, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return []*story{}, err
	}

	newStories := []*story{}
	for _, s := range stories {
		found := false
		for _, ku := range knownURLs {
			if ku == s.URL {
				found = true
				break
			}
		}
		if !found {
			newStories = append(newStories, s)
		}
	}
	return newStories, nil
}
