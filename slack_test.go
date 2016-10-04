package main_test

import (
	"fmt"

	"github.com/chck/break-upper/config"
	"github.com/nlopes/slack"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Slack", func() {
	var (
		client *slack.Client
	)

	BeforeEach(func() {
		client = slack.New(config.Load().Tokens.Slack)
	})

	Describe("Getting stars", func() {
		Context("by default parameters", func() {
			It("should be present", func() {
				stars, _, _ := client.ListStars(slack.NewStarsParameters())
				items, _, _ := client.GetStarred(slack.NewStarsParameters())
				for _, stars := range items {
					fmt.Println(stars.Channel)
				}
				Expect(stars).NotTo(BeEmpty())
			})
		})
	})
})
