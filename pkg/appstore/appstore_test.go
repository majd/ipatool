package appstore

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAppStore(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "App Store Suite")
}
