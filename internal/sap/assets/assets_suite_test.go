package assets

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAssetsGinkgo(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "SAP Assets Suite")
}
