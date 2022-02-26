prefix ?= /usr/local
bindir = $(prefix)/bin

build:
	swift build --product ipatool --configuration release --triple arm64-apple-macosx
	swift build --product ipatool --configuration release --triple x86_64-apple-macosx
	lipo -create -output .build/ipatool .build/arm64-apple-macosx/release/ipatool .build/x86_64-apple-macosx/release/ipatool

install: build
	install -d "$(bindir)"
	install ".build/ipatool" "$(bindir)"

uninstall:
	rm -rf "$(bindir)/ipatool"

clean:
	rm -rf .build

.PHONY: build install uninstall clean
