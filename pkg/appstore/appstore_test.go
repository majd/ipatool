package appstore

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"testing"
)

func TestAppStore(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "App Store Suite")
}
