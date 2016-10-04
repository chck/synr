package main_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestBreakUpper(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "BreakUpper Suite")
}
