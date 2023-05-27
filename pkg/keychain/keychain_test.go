package keychain

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestKeychain(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Keychain Suite")
}
