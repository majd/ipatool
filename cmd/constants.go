package cmd

import "github.com/thediveo/enumflag/v2"

type OutputFormat enumflag.Flag

const (
	OutputFormatText OutputFormat = iota
	OutputFormatJSON
)

const (
	ConfigDirectoryName = ".ipatool"
	CookieJarFileName   = "cookies"
	KeychainServiceName = "ipatool-auth.service"
)
