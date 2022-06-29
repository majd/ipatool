prefix ?= /usr/local
bindir = $(prefix)/bin

lint:
	swiftlint lint --config .swiftlint.yml --path Sources/ Tests/m Plugins/

build: lint
	swift build -c release --arch arm64
	swift build -c release --arch x86_64
	lipo -create -output .build/ipatool .build/arm64-apple-macosx/release/ipatool .build/x86_64-apple-macosx/release/ipatool

install: build
	install -d "$(bindir)"
	install ".build/ipatool" "$(bindir)"

uninstall:
	rm -rf "$(bindir)/ipatool"

clean:
	rm -rf .build

.PHONY: build install uninstall clean
