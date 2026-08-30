package appstore

import (
	"bytes"
	"encoding/json"
	"time"

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
		purchaseDate := time.Unix(1_700_000_000, 0).UTC()
		app := App{
			ID:           42,
			BundleID:     "app.bundle.id",
			Name:         "app name",
			Version:      "1.0",
			Price:        0,
			PurchaseDate: purchaseDate,
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
		Expect(out["purchaseDate"]).To(Equal(purchaseDate.Format(time.RFC3339)))
	})

	It("omits an unknown purchase date", func() {
		buffer := bytes.NewBuffer([]byte{})
		logger := zerolog.New(buffer)
		app := App{ID: 42}

		event := logger.Log()
		app.MarshalZerologObject(event)
		event.Send()

		var out map[string]interface{}
		err := json.Unmarshal(buffer.Bytes(), &out)
		Expect(err).ToNot(HaveOccurred())
		Expect(out).ToNot(HaveKey("purchaseDate"))
	})

	It("formats ipa name correctly", func() {
		app := App{
			ID:       42,
			BundleID: "app.bundle-id1",
			Name:     "      some  app&symb.ols2  !!!",
			Version:  "1.0",
			Price:    0,
		}

		Expect(fileName(app, "1.0")).To(Equal("app.bundle-id1_42_1.0.ipa"))
		Expect(packageFileName(app, "1.0", PlatformMacOS)).To(Equal("app.bundle-id1_42_1.0.pkg"))
	})
})
