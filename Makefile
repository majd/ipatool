prefix ?= /usr/local
bindir = $(prefix)/bin

build:
	xcrun swift build -c release --arch arm64 --arch x86_64

install: build
	install -d "$(bindir)"
	install ".build/apple/Products/Release/ipatool" "$(bindir)"

uninstall:
	rm -rf "$(bindir)/ipatool"

clean:
	rm -rf .build

.PHONY: build install uninstall clean
