package main

import (
	"fmt"
	"os"

	"github.com/chck/break-upper/chatwork"
	"github.com/chck/break-upper/slack"

	"github.com/chck/break-upper/config"
	flags "github.com/jessevdk/go-flags"
)

type options struct {
	ChatName string `short:"c" long:"chatname" description:"A name of chat you'd like to brake up"`
}

func chatName() string {
	opts := &options{}
	parser := flags.NewParser(opts, flags.PrintErrors)
	parser.Name = "break-upper"
	parser.Usage = "-c slack"
	args, _ := parser.Parse()

	if len(args) != 0 || opts.ChatName == "" {
		parser.WriteHelp(os.Stdout)
		os.Exit(1)
	}

	return opts.ChatName
}

func main() {
	tokens := config.Load().Tokens

	switch chatName() {
	case "chatwork":
		chatwork.New(tokens.Chatwork)
	case "slack":
		client := slack.New(tokens.Slack)
		channels, _ := client.GetChannels(false)
		starredIDs := slack.StarredChannelIDs(client)
		for _, channel := range channels {
			slack.MayBeLeaveChannel(client, channel, starredIDs)
		}
	}
	fmt.Println("++++++++++++++++++++++++")
	fmt.Println("Broken up!!")
}