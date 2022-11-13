package appstore

import (
	"bytes"
	"encoding/json"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rs/zerolog"
)

var _ = Describe("App", func() {
	It("marshals apps array", func() {
		apps := Apps{
			{
				ID:       42,
				BundleID: "app.bundle.id",
				Name:     "app name",
				Version:  "1.0",
				Price:    0,
			},
			{
				ID:       1,
				BundleID: "app.bundle.id2",
				Name:     "app name2",
				Version:  "2.0",
				Price:    0.99,
			},
		}

		buffer := bytes.NewBuffer([]byte{})
		logger := zerolog.New(buffer)
		event := logger.Log().Array("apps", apps)
		event.Send()

		var out map[string]interface{}
		err := json.Unmarshal(buffer.Bytes(), &out)
		Expect(err).ToNot(HaveOccurred())
		Expect(out["apps"]).To(HaveLen(2))
	})

	It("marshalls app object", func() {
		app := App{
			ID:       42,
			BundleID: "app.bundle.id",
			Name:     "app name",
			Version:  "1.0",
			Price:    0,
		}

		buffer := bytes.NewBuffer([]byte{})
		logger := zerolog.New(buffer)
		event := logger.Log()
		app.MarshalZerologObject(event)
		event.Send()

		var out map[string]interface{}
		err := json.Unmarshal(buffer.Bytes(), &out)
		Expect(err).ToNot(HaveOccurred())

		Expect(out["id"]).To(Equal(float64(42)))
		Expect(out["bundleID"]).To(Equal("app.bundle.id"))
		Expect(out["name"]).To(Equal("app name"))
		Expect(out["version"]).To(Equal("1.0"))
		Expect(out["price"]).To(Equal(float64(0)))
	})
})
