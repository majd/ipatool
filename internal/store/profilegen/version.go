package profilegen

// FrameworkUUID and PluginUUID pin the Apple images consumed by the bootstrap.
const FrameworkUUID = "5DFC37A5-4689-3F9D-9655-D4774E1D8D08"
const PluginUUID = "B8C07F94-4868-3CD7-A59A-F01E8C24C1B7"

// Increment bootstrapRevision whenever generation or the profile format changes.
const bootstrapRevision = "3"

// Version identifies the pinned inputs and bootstrap implementation.
func Version() string {
	return "macos-27-26A434-unicorn-" + bootstrapRevision + "-" + digest([]byte(FrameworkUUID + PluginUUID))[:16]
}
