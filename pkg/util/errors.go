package util

import "github.com/pkg/errors"

var (
	ErrEmptyNetworkInterfaces  = errors.New("could not find network interfaces")
	ErrGetNetworkInterfaces    = errors.New("failed to get network interfaces")
	ErrInvalidNetworkInterface = errors.New("could not find network interfaces with a valid MAC address")
	ErrSlicesLengthMismatch    = errors.New("slices have different lengths")
)
