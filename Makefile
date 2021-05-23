prefix ?= /usr/local
bindir = $(prefix)/bin

build:
	xcodebuild -scheme ipatool -configuration release -archivePath .build archive

install: build
	install -d "$(bindir)"
	install ".build.xcarchive/Products/$(bindir)/ipatool" "$(bindir)"

uninstall:
	rm -rf "$(bindir)/ipatool"

clean:
	rm -rf .build.xcarchive

.PHONY: build install uninstall clean
