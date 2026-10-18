package emulator

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestStoreRuntime(t *testing.T) { RegisterFailHandler(Fail); RunSpecs(t, "Store Runtime") }
