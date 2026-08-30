package machine

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestMachineGinkgo(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "SAP Machine Suite")
}
