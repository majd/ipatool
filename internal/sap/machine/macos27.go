package machine

// macOS27SharedCacheFunctions maps the external function pointers used by the
// AppleMediaServices image extracted from the macOS 27.0.1 (26A434) arm64e
// shared cache. The extraction has its cache slide applied, so these values are
// stable for this exact build and are replaced with emulator services at load
// time.
var macOS27SharedCacheFunctions = map[uint64]string{
	0x0000000180675ce8: "_malloc",
	0x00000001806766f0: "_free",
	0x00000001806cf570: "_dispatch_once",
	0x000000018071acc8: "_sysctlbyname",
	0x000000018071bec4: "_gettimeofday",
	0x0000000180721fb8: "_arc4random",
	0x00000001807238a4: "___memset_chk",
	0x00000001807903c4: "_abort",
	0x0000000180849604: "___error",
	0x0000000180889bc4: "_pthread_once",
	0x000000018088a6f8: "_pthread_rwlock_unlock",
	0x000000018088a7c0: "_pthread_rwlock_wrlock",
	0x000000018088b2d8: "_pthread_rwlock_init",
	0x0000000180894b40: "_strlen",
	0x00000001808cf540: "_CFStringCreateWithCString",
	0x00000001808d4200: "_CFStringGetLength",
	0x00000001808d44dc: "_CFStringGetCString",
	0x00000001808d7278: "_CFRelease",
	0x00000001808e89e8: "_CFDataGetLength",
	0x0000000184d6b4cc: "_IOServiceMatching",
	0x0000000184d6c168: "_IOServiceGetMatchingServices",
	0x0000000184d6c4fc: "_IOIteratorNext",
	0x0000000184d6cb50: "_IOObjectRelease",
}

var macOS27SharedCacheSlots = map[uint64]string{
	0x00000001e02205c0: "___stack_chk_guard",
	0x00000001e800c958: "___chkstk_darwin",
}
