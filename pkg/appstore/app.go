package appstore

import (
	"fmt"
	"github.com/rs/zerolog"
	"regexp"
	"strings"
)

type App struct {
	ID       int64   `json:"trackId,omitempty"`
	BundleID string  `json:"bundleId,omitempty"`
	Name     string  `json:"trackName,omitempty"`
	Version  string  `json:"version,omitempty"`
	Price    float64 `json:"price,omitempty"`
}

type Apps []App

func (apps Apps) MarshalZerologArray(a *zerolog.Array) {
	for _, app := range apps {
		a.Object(app)
	}
}

func (a App) MarshalZerologObject(event *zerolog.Event) {
	event.
		Int64("id", a.ID).
		Str("bundleID", a.BundleID).
		Str("name", a.Name).
		Str("version", a.Version).
		Float64("price", a.Price)
}

func (a App) GetIPAName() string {
	return fmt.Sprintf("%s+%s+%d+%s.ipa",
		a.cleanName(a.BundleID),
		a.cleanName(a.Name),
		a.ID,
		a.cleanName(a.Version))
}

var cleanRegex1 = regexp.MustCompile("[^-\\w.]")
var cleanRegex2 = regexp.MustCompile("\\s+")

func (a App) cleanName(name string) string {
	name = cleanRegex1.ReplaceAllString(name, " ")
	name = strings.TrimSpace(name)
	name = cleanRegex2.ReplaceAllString(name, "_")
	return name
}
