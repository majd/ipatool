prefix ?= /usr/local
bindir = $(prefix)/bin

build:
	swift build --configuration release

install: build
	install -d "$(bindir)"
	install ".build/release/ipatool" "$(bindir)"

uninstall:
	rm -rf "$(bindir)/ipatool"

clean:
	rm -rf .build

.PHONY: build install uninstall clean
