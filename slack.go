package main

import (
	"fmt"
	"github.com/slack-go/slack"
	"os"
)

func sendMessages(msg []byte) error {
	whmsg := &slack.WebhookMessage{Text: string(msg)}
	slackURL := fmt.Sprintf("https://hooks.slack.com/services/%s/%s/%s", os.Getenv("FIRST_SEGMENT"), os.Getenv("SECOND_SEGMENT"), os.Getenv("THIRD_SEGMENT"))
	return slack.PostWebhook(slackURL, whmsg)
}
