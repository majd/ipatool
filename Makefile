prefix ?= /usr/local
bindir = $(prefix)/bin

define ios_entitlements
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
	<dict>
		<key>keychain-access-groups</key>
		<array>
			<string>ipatool</string>
		</array>
		<key>application-identifier</key>
		<string>ipatool</string>
		<key>platform-application</key>
		<true />
		<key>com.apple.private.security.container-required</key>
		<false />
	</dict>
</plist>
endef

lint:
	swiftlint lint --config .swiftlint.yml Sources/ Tests/m Plugins/

build-macos: lint
	swift build -c release --arch arm64
	swift build -c release --arch x86_64
	lipo -create -output .build/ipatool .build/arm64-apple-macosx/release/ipatool .build/x86_64-apple-macosx/release/ipatool

build-ios: lint
	xcodegen -s ios-project.yml
	xcodebuild archive -scheme CLI \
            -sdk iphoneos \
            -configuration Release \
            -derivedDataPath .build \
            -archivePath .build/ipatool.xcarchive \
            CODE_SIGN_IDENTITY='' \
            CODE_SIGNING_REQUIRED=NO \
            CODE_SIGN_ENTITLEMENTS='' \
            CODE_SIGNING_ALLOWED=NO
	cp .build/ipatool.xcarchive/Products/Applications/CLI.app/CLI .build/ipatool
	echo "$$ios_entitlements" > "${PWD}/.build/entitlements.xml"
	ldid -S.build/entitlements.xml ./.build/ipatool

install-macos: build
	install -d "$(bindir)"
	install ".build/ipatool" "$(bindir)"

uninstall-macos:
	rm -rf "$(bindir)/ipatool"

clean:
	rm -rf .build

.PHONY: build-macos install-macos uninstall-macos clean

export ios_entitlements