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
	ChatName    string `short:"c" long:"chatname" description:"A name of chat you'd like to brake up"`
	DryRun      bool   `short:"d" long:"dry-run" description:"Pre-running break up unnessesary chat rooms"`
	BeforeMonth int    `short:"m" long:"before-month" description:"Set X month elapsed when Last of talking date to break up: DEFAULT 1 MONTH AGO"`
}

func cmdOpts() *options {
	opts := &options{}
	parser := flags.NewParser(opts, flags.PrintErrors)
	parser.Name = "break-upper"
	parser.Usage = "-c slack"
	args, _ := parser.Parse()

	if len(args) != 0 || opts.ChatName == "" {
		parser.WriteHelp(os.Stdout)
		os.Exit(1)
	}

	return opts
}

func main() {
	fmt.Println("Start breaking!!")
	fmt.Println("++++++++++++++++++++++++")
	tokens := config.Load().Tokens

	opts := cmdOpts()

	switch opts.ChatName {
	case "chatwork":
		client := chatwork.New(tokens.Chatwork)
		rooms, _ := client.GetRooms()
		for _, room := range rooms {
			chatwork.MayBeLeaveRoom(opts.DryRun, opts.BeforeMonth, client, &room)
		}
	case "slack":
		client := slack.New(tokens.Slack)
		channels, _ := client.GetChannels(false)
		starredIDs := slack.StarredChannelIDs(client)
		for _, channel := range channels {
			slack.MayBeLeaveChannel(opts.DryRun, opts.BeforeMonth, client, channel, starredIDs)
		}
	}
	fmt.Println("++++++++++++++++++++++++")
	fmt.Println("Broken up!!")
}
