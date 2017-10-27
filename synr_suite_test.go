package main_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestSynr(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Synr Suite")
}
