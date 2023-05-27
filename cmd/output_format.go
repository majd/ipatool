package cmd

import (
	"fmt"

	"github.com/thediveo/enumflag/v2"
)

type OutputFormat enumflag.Flag

const (
	OutputFormatText OutputFormat = iota
	OutputFormatJSON
)

func OutputFormatFromString(value string) (OutputFormat, error) {
	switch value {
	case "json":
		return OutputFormatJSON, nil
	case "text":
		return OutputFormatText, nil
	default:
		return OutputFormatJSON, fmt.Errorf("invalid output format '%s'", value)
	}
}
