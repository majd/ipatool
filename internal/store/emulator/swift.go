package emulator

import (
	"encoding/binary"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	profileformat "github.com/majd/ipatool/v2/internal/store/profile/format"

	"github.com/majd/ipatool/v2/internal/sap/unicorn"
	"golang.org/x/text/unicode/norm"
)

// The runtime owns all allocations until Close, including Swift references.
// These are ABI services, not replacements for AMS request or response methods.
func (r *Runtime) swiftSymbol(name string) bool {
	if r.asyncSymbol(name) {
		return true
	}

	switch name {
	case "$ss27_stringCompareWithSmolCheck__9expectingSbs11_StringGutsV_ADs01_G16ComparisonResultOtF":
		left, right := norm.NFC.String(r.swiftString(r.arg(0), r.arg(1))), norm.NFC.String(r.swiftString(r.arg(2), r.arg(3)))

		switch r.arg(4) {
		case 0:
			r.set(boolValue(left == right))
		case 1:
			r.set(boolValue(left < right))
		default:
			panic("unsupported Swift string comparison")
		}
	case "$ss18_DictionaryStorageC6resize8original8capacity4moveAByxq_Gs05__RawaB0C_SiSbtFZ":
		storageType, capacity := r.arg(3), r.arg(1)

		var metadata uint64

		for key, layout := range r.profile.SwiftDictionaries {
			if layout.Metadata == storageType {
				metadata, _ = strconv.ParseUint(key, 10, 64)

				break
			}
		}

		if metadata == 0 {
			panic(fmt.Sprintf("unprepared dictionary storage %#x", storageType))
		}

		slot := r.alloc(8)
		r.write64(slot, r.profile.Symbols["_swiftEmptyDictionarySingleton"])
		r.ensureUniqueDictionary(slot, false, capacity, metadata)
		r.set(r.read64(slot))
	case "swift_dynamicCast":
		destination, source, sourceType, targetType := r.arg(0), r.arg(1), r.arg(2), r.arg(3)
		if sourceType == r.profile.Symbols["$ss11AnyHashableVN"] && targetType == r.profile.Symbols["$sSSN"] {
			if r.read64(source+24) != r.profile.Bridges["anyHashableStringMetadata"] {
				panic("unsupported AnyHashable cast box")
			}

			sourceType = targetType
		} else if sourceType == r.profile.Symbols["$sypN"]+8 {
			sourceType = r.read64(source + 24)
		}

		if sourceType == targetType {
			size := r.read64(r.read64(sourceType-8) + 64)
			if size > 24 {
				panic("unsupported Swift cast value size")
			}

			data, err := r.engine.MemRead(source, size)
			if err != nil {
				panic(err)
			}

			if err = r.MemWrite(destination, data); err != nil {
				panic(err)
			}

			r.set(1)
		} else if targetType == r.profile.Symbols["$sSSN"] && (sourceType == r.profile.Symbols["$sSiN"] || sourceType == r.profile.Symbols["$sSbN"] || sourceType == r.profile.Symbols["$sSdN"]) && r.arg(4)&1 == 0 {
			r.set(0)
		} else {
			panic(fmt.Sprintf("unsupported Swift cast metadata %#x -> %#x flags=%d", sourceType, targetType, r.arg(4)))
		}
	case "$ss17CodingUserInfoKeyVwCP", "$sSSwCP", "$sSSwcp", "$sSSwca":
		// The pinned runtime shares this witness with String: two words,
		// followed by a retain that our scoped allocation lifetime subsumes.
		data, err := r.engine.MemRead(r.arg(1), 16)
		if err != nil {
			panic(err)
		}

		if err = r.MemWrite(r.arg(0), data); err != nil {
			panic(err)
		}
	case "$ss11AnyHashableVwxx", "$sSSwxx":
		// References remain alive until this runtime closes.
	case "$ss11AnyHashableVwcp", "$ss11AnyHashableVwca":
		// This bridge currently constructs only inline String-backed boxes.
		if r.read64(r.arg(1)+24) != r.profile.Bridges["anyHashableStringMetadata"] {
			panic("unsupported AnyHashable copy layout")
		}

		data, err := r.engine.MemRead(r.arg(1), 40)
		if err != nil {
			panic(err)
		}

		if err = r.MemWrite(r.arg(0), data); err != nil {
			panic(err)
		}
	case "$sSD10FoundationE36_unconditionallyBridgeFromObjectiveCySDyxq_GSo12NSDictionaryCSgFZ":
		if (r.arg(1) != r.profile.Symbols["$sSSN"] && r.arg(1) != r.profile.Symbols["$ss11AnyHashableVN"]) || (r.arg(2) != r.profile.Symbols["$sypN"]+8 && r.arg(2) != r.profile.Symbols["$sSSN"]) {
			panic(fmt.Sprintf("unsupported dictionary bridge metadata %#x/%#x", r.arg(1), r.arg(2)))
		}

		r.set(r.bridgeDictionaryToSwift(r.arg(0), r.arg(1), r.arg(2)))

	case "$sSo13os_log_type_ta0A0E5errorABvgZ":
		r.set(16)
	case "$sSo13os_log_type_ta0A0E5faultABvgZ":
		r.set(17)
	case "$sSo13os_log_type_ta0A0E4infoABvgZ":
		r.set(1)
	case "$sSo13os_log_type_ta0A0E5debugABvgZ":
		r.set(2)
	case "$sSo13os_log_type_ta0A0E7defaultABvgZ":
		r.set(0)

	case "$sSS6appendyySSF":
		self, _ := r.engine.RegRead(unicorn.RegX0 + 20)
		value := r.swiftString(r.read64(self), r.read64(self+8)) + r.swiftString(r.arg(0), r.arg(1))
		r.returnSwiftString(value)
		r.write64(self, r.arg(0))
		r.write64(self+8, r.arg(1))

	case "$ss9_typeName_9qualifiedSSypXp_SbtF":
		name := r.classNames[r.arg(0)]
		if name == "" && r.read64(r.arg(0)) == 0x305 {
			name = r.classNames[r.read64(r.arg(0)+8)]
		}

		if name == "" {
			panic(fmt.Sprintf("unprepared Swift type name %#x", r.arg(0)))
		}

		r.returnSwiftString(name)

	case "$ss9TaskLocalC12wrappedValueAByxGx_tcfc":
		self, _ := r.engine.RegRead(unicorn.RegX0 + 20)
		metadata := r.read64(self)
		valueType := r.read64(metadata + 80)

		size := r.read64(r.read64(valueType-8) + 64)
		if size > r.allocations[self]-16 {
			panic("TaskLocal value exceeds allocation")
		}

		data, err := r.engine.MemRead(r.arg(0), size)
		if err != nil {
			panic(err)
		}

		if err = r.MemWrite(self+16, data); err != nil {
			panic(err)
		}

		r.objects[self] = &Object{Class: "HostSwiftTaskLocal", Properties: map[string]uint64{"valueSize": size, "defaultValue": self + 16}}
		r.set(self)
	case "$ss9TaskLocalC3getxyF", "$ss9TaskLocalC12wrappedValuexvg":
		self, _ := r.engine.RegRead(unicorn.RegX0 + 20)

		obj := r.objects[self]
		if obj == nil {
			panic("unknown TaskLocal")
		}

		out, _ := r.engine.RegRead(unicorn.RegX0 + 8)

		source := r.taskLocalValue(self)
		if source == 0 {
			source = obj.Properties["defaultValue"]
		}

		data, err := r.engine.MemRead(source, obj.Properties["valueSize"])
		if err != nil {
			panic(err)
		}

		if err = r.MemWrite(out, data); err != nil {
			panic(err)
		}

	case "$sSo9OS_os_logC0B0E9subsystem8categoryABSS_SStcfC":
		r.set(r.New("HostOSLog", nil))
	case "$s10Foundation10URLRequestV8setValue_18forHTTPHeaderFieldySSSg_SStF":
		copied := r.mutableSwiftRequest()

		object := r.objects[copied]
		if object.Properties["allHTTPHeaderFields"] == 0 {
			object.Properties["allHTTPHeaderFields"] = r.New("NSMutableDictionary", map[string]uint64{})
		}

		fields := r.objects[object.Properties["allHTTPHeaderFields"]].Value.(map[string]uint64)
		key := http.CanonicalHeaderKey(r.swiftString(r.arg(2), r.arg(3)))

		if r.arg(1) == 0 {
			delete(fields, key)
		} else {
			fields[key] = r.New("NSString", r.swiftString(r.arg(0), r.arg(1)))
		}

	case "swift_initWithCopy_strong", "swift_initWithTake_strong", "swift_assignWithCopy_strong", "swift_assignWithTake_strong":
		r.write64(r.arg(0), r.read64(r.arg(1)))
	case "$s10Foundation3URLV14absoluteStringSSvg":
		self, _ := r.engine.RegRead(unicorn.RegX0 + 20)
		r.returnSwiftString(r.String(r.read64(self)))

	case "$s10Foundation10URLRequestV3urlAA3URLVSgvg":
		self, _ := r.engine.RegRead(unicorn.RegX0 + 20)

		object := r.objects[r.read64(self)]
		if object == nil {
			panic("invalid Swift URLRequest value")
		}

		out, _ := r.engine.RegRead(unicorn.RegX0 + 8)
		r.write64(out, object.Properties["URL"])
		r.write64(out+8, 0)

	case "swift_getSingletonMetadata":
		value := r.profile.SwiftSingletons[strconv.FormatUint(r.arg(1), 10)]
		if value == 0 {
			panic(fmt.Sprintf("unprepared singleton metadata %#x", r.arg(1)))
		}

		r.set(value)
		_ = r.engine.RegWrite(unicorn.RegX1, 0)

	case "$s10Foundation22_convertErrorToNSErrorySo0E0Cs0C0_pF":
		value := r.arg(0)
		for depth := 0; depth < 16; depth++ {
			object := r.objects[value]
			if object == nil {
				panic("unknown Swift error value")
			}

			if r.kindOf(object.Class, "NSError") {
				r.set(value)

				return true
			}

			if object.Class != "HostSwiftError" || object.Properties["metadata"] != r.profile.SwiftTypes[strconv.FormatUint(0x1a2e1c7fa, 10)] {
				panic(fmt.Sprintf("unsupported Swift error bridge %s metadata %#x", object.Class, object.Properties["metadata"]))
			}

			value = r.read64(object.Properties["value"])
		}

		panic("recursive Swift error box")

	case "$ss38_bridgeAnythingNonVerbatimToObjectiveCyyXlxnlF":
		source, metadata := r.arg(0), r.arg(1)
		switch metadata {
		case r.profile.Symbols["$sSSN"]:
			r.set(r.New("NSString", r.swiftString(r.read64(source), r.read64(source+8))))
		case r.profile.Symbols["$sSiN"]:
			r.set(r.New("NSNumber", int64(r.read64(source))))
		default:
			panic(fmt.Sprintf("unsupported nonverbatim bridge metadata %#x", metadata))
		}

	case "$sSD10FoundationE19_bridgeToObjectiveCSo12NSDictionaryCyF":
		r.set(r.bridgeDictionaryToObjC(r.arg(0), r.arg(1), r.arg(2)))

	case "$sSS4hash4intoys6HasherVz_tF":
		value := norm.NFC.String(r.swiftString(r.arg(1), r.arg(2)))
		r.hashBytes(r.arg(0), append([]byte(value), 255))

	case "swift_allocError":
		metadata, witness, source := r.arg(0), r.arg(1), r.arg(2)

		size := r.read64(r.read64(metadata-8) + 64)
		if size > 1<<20 {
			panic("Swift error exceeds budget")
		}

		value := r.alloc(size)
		box := r.New("HostSwiftError", nil)

		r.objects[box].Properties = map[string]uint64{"metadata": metadata, "witness": witness, "value": value}
		if source != 0 {
			data, err := r.engine.MemRead(source, size)
			if err != nil {
				panic(err)
			}

			if err = r.MemWrite(value, data); err != nil {
				panic(err)
			}
		}

		r.set(box)
		_ = r.engine.RegWrite(unicorn.RegX1, value)
	case "swift_getErrorValue":
		box := r.objects[r.arg(0)]
		if box == nil || box.Class != "HostSwiftError" {
			panic("unsupported unboxed Swift error")
		}

		out := r.arg(2)
		r.write64(out, box.Properties["value"])
		r.write64(out+8, box.Properties["metadata"])
		r.write64(out+16, box.Properties["witness"])
	case "swift_errorRetain", "swift_errorRelease", "swift_willThrow":

	case "$s10Foundation10URLRequestV36_unconditionallyBridgeFromObjectiveCyACSo12NSURLRequestCSgFZ":
		value := r.arg(0)
		if obj := r.objects[value]; obj == nil || !r.kindOf(obj.Class, "NSURLRequest") {
			panic("invalid Swift request bridge input")
		}

		out, _ := r.engine.RegRead(unicorn.RegX0 + 8)
		r.write64(out, value)
	case "$s10Foundation10URLRequestV19_bridgeToObjectiveCSo12NSURLRequestCyF":
		self, _ := r.engine.RegRead(unicorn.RegX0 + 20)
		r.set(r.read64(self))

	case "$sScPwst":
		if r.arg(2) != 1 || r.arg(1) > 1 {
			panic("unsupported task priority optional tag")
		}

		size := r.read64(r.read64(r.arg(3)-8) + 64)
		if size != 1 {
			panic("unexpected priority size")
		}

		if err := r.MemWrite(r.arg(0)+size, []byte{byte(r.arg(1))}); err != nil {
			panic(err)
		}
	case "$sScPwet":
		size := r.read64(r.read64(r.arg(2)-8) + 64)

		data, err := r.engine.MemRead(r.arg(0)+size, 1)
		if err != nil {
			panic(err)
		}

		r.set(uint64(data[0]))

	case "$s10Foundation3URLV06isFileB0Sbvg":
		self, _ := r.engine.RegRead(unicorn.RegX0 + 20)

		u, err := url.Parse(r.String(r.read64(self)))
		if err != nil {
			panic(err)
		}

		r.set(boolValue(u.Scheme == "file"))
	case "$s10Foundation3URLV19_bridgeToObjectiveCSo5NSURLCyF":
		self, _ := r.engine.RegRead(unicorn.RegX0 + 20)
		r.set(r.read64(self))

	case "$sSqwta", "$sSqwca", "$sSqwcp", "$sSqwtk", "swift_cvw_initWithCopy", "swift_cvw_assignWithCopy", "swift_cvw_initWithTake", "swift_cvw_assignWithTake":
		size := r.read64(r.read64(r.arg(2)-8) + 64)
		if size > 1024 {
			panic("Swift value copy exceeds budget")
		}

		data, err := r.engine.MemRead(r.arg(1), size)
		if err != nil {
			panic(err)
		}

		if err = r.MemWrite(r.arg(0), data); err != nil {
			panic(err)
		}

	case "$s10Foundation3URLVwst":
		if r.arg(2) != 1 || r.arg(1) > 1 {
			panic("unsupported URL optional tag")
		}

		if r.arg(1) == 1 {
			r.write64(r.arg(0), 0)
			r.write64(r.arg(0)+8, 0)
		}
	case "$s10Foundation3URLVwet":
		if r.arg(1) != 1 {
			panic("unsupported URL optional tag")
		}

		r.set(boolValue(r.read64(r.arg(0)) == 0))

	case "$s10Foundation3URLV9appending10components13directoryHintACxd_AC09DirectoryF0OtSyRzlF", "$s10Foundation3URLV6append10components13directoryHintyxd_AC09DirectoryF0OtSyRzlF":
		self, _ := r.engine.RegRead(unicorn.RegX0 + 20)
		array := r.arg(0)

		count := r.read64(array + 16)
		if count > 32 {
			panic("URL component count exceeds budget")
		}

		value := r.String(r.read64(self))

		for i := uint64(0); i < count; i++ {
			p := array + 32 + i*16
			component := r.swiftString(r.read64(p), r.read64(p+8))

			var err error

			value, err = url.JoinPath(value, component)
			if err != nil {
				panic(err)
			}
		}

		if strings.Contains(name, "9appending") {
			self, _ = r.engine.RegRead(unicorn.RegX0 + 8)
		}

		r.write64(self, r.New("NSURL", value))
		r.write64(self+8, 0)

	case "$s10Foundation3URLV36_unconditionallyBridgeFromObjectiveCyACSo5NSURLCSgFZ":
		if r.objects[r.arg(0)] == nil || r.objects[r.arg(0)].Class != "NSURL" {
			panic("invalid Swift URL bridge input")
		}

		out, _ := r.engine.RegRead(unicorn.RegX0 + 8)
		r.write64(out, r.arg(0))
		r.write64(out+8, 0)

	case "$s10ObjectiveC8SelectorV2eeoiySbAC_ACtFZ":
		r.set(boolValue(r.arg(0) == r.arg(1)))
	case "$ss17_NativeDictionaryV12ensureUnique02isD08capacityS2b_SitF":
		self, _ := r.engine.RegRead(unicorn.RegX0 + 20)
		r.ensureUniqueDictionary(self, r.arg(0) != 0, r.arg(1), r.arg(2))

	case "$ss6HasherV5_seedABSi_tcfC":
		out, _ := r.engine.RegRead(unicorn.RegX0 + 8)
		r.initHasher(out, r.arg(0))
	case "$ss6HasherV8_combineyySuF":
		self, _ := r.engine.RegRead(unicorn.RegX0 + 20)
		r.hashWord(self, r.arg(0))
	case "$ss6HasherV9_finalizeSiyF":
		self, _ := r.engine.RegRead(unicorn.RegX0 + 20)
		r.set(r.hashCompress(self, r.read64(self), true))
	case "$s10ObjectiveC8SelectorV4hash4intoys6HasherVz_tF":
		r.hashWord(r.arg(0), r.arg(1))

	case "$sSo17OS_dispatch_queueC8DispatchE5async5group3qos5flags7executeySo0a1_b1_F0CSg_AC0D3QoSVAC0D13WorkItemFlagsVyyXBtF":
		if r.arg(0) != 0 {
			panic("Swift dispatch group unsupported")
		}

		queue, _ := r.engine.RegRead(unicorn.RegX0 + 20)
		r.copyBlock(r.arg(3), func(block uint64) { r.queuePending(block, queue); r.set(0) })

	case "$sSo17OS_dispatch_queueC8DispatchE5label3qos10attributes20autoreleaseFrequency6targetABSS_AC0D3QoSVAbCE10AttributesVAbCE011AutoreleaseI0OABSgtcfC", "$sSo24OS_dispatch_queue_serialC8DispatchE5label3qos10attributes20autoreleaseFrequency6targetABSS_AC0E3QoSVAbCE10AttributesVSo0a1_b1_C0CACE011AutoreleaseJ0OANSgtcfC":
		target := r.arg(5)
		queue := r.New("HostQueue", r.swiftString(r.arg(0), r.arg(1)))
		r.objects[queue].Properties["targetQueue"] = target
		r.set(queue)
	case "$ss10SetAlgebraPyxqd__ncSTRd__7ElementQyd__ACRtzlufCTj":
		if (r.arg(3) != r.profile.SwiftAccessors["$sSo24OS_dispatch_queue_serialC8DispatchE10AttributesVMa"] && r.arg(3) != r.profile.SwiftAccessors["$sSo17OS_dispatch_queueC8DispatchE10AttributesVMa"] && r.arg(3) != r.profile.SwiftAccessors["$s8Dispatch0A13WorkItemFlagsVMa"]) || r.read64(r.read64(r.arg(0))+16) != 0 {
			panic("unsupported SetAlgebra initializer")
		}

		out, _ := r.engine.RegRead(unicorn.RegX0 + 8)
		r.write64(out, 0)
	case "$s8Dispatch0A3QoSV11unspecifiedACvgZ":
		out, _ := r.engine.RegRead(unicorn.RegX0 + 8)
		r.write64(out, 5)
		r.write64(out+8, 0)
	case "swift_isUniquelyReferenced_nonNull_native", "swift_isUniquelyReferenced_nonNull_bridgeObject":
		// Conservatively copy; host handles can retain aliases for VM lifetime.
		r.set(0)
	case "swift_retain_n", "swift_bridgeObjectRetain_n", "swift_retain", "swift_bridgeObjectRetain", "swift_unknownObjectRetain":
	case "swift_destroy_strong", "$ss15CollectionOfOneV8IteratorVwxx", "swift_pod_destroy", "swift_arrayDestroy", "swift_deallocPartialClassInstance", "swift_cvw_destroy", "swift_noop_void_return", "swift_release_n", "swift_bridgeObjectRelease_n", "swift_release", "swift_bridgeObjectRelease", "swift_unknownObjectRelease", "swift_deallocObject":
	case "swift_arrayInitWithCopy", "swift_arrayInitWithTakeFrontToBack", "swift_arrayInitWithTakeBackToFront":
		destination, source, count, metadata := r.arg(0), r.arg(1), r.arg(2), r.arg(3)
		if count == 0 {
			r.set(destination)

			break
		}

		witnesses := r.read64(metadata - 8)

		stride := r.read64(witnesses + 72)
		if count > 65536 || stride > 1<<20 {
			panic("Swift array copy exceeds budget")
		}

		index := uint64(0)

		offset := uint64(16)
		if name != "swift_arrayInitWithCopy" {
			offset = 32
		}

		var next func()
		next = func() {
			if index == count {
				r.set(destination)

				return
			}

			i := index
			if name == "swift_arrayInitWithTakeBackToFront" {
				i = count - index - 1
			}

			index++

			r.callbackFunction(r.read64(witnesses+offset), []uint64{destination + i*stride, source + i*stride, metadata}, next)
		}
		next()
	case "swift_allocObject":
		metadata, size := r.arg(0), r.arg(1)
		if size < 16 {
			panic("invalid Swift object size")
		}

		object := r.alloc(size)
		r.write64(object, metadata)
		r.write64(object+8, 3) // inline refcount: one strong reference, live object
		r.set(object)
	case "swift_getForeignTypeMetadata":
		candidate := r.arg(1)
		if value := r.profile.SwiftForeign[strconv.FormatUint(candidate, 10)]; value != 0 {
			r.set(value)
			_ = r.engine.RegWrite(unicorn.RegX1, 0)

			break
		}
		// AMSErrorCode: the pinned native runtime returns this complete, static enum
		// metadata unchanged. Do not generalize to foreign classes with initializers.
		if candidate != 0x1eaa52f08 || r.read64(candidate) != 0x201 {
			panic(fmt.Sprintf("unsupported foreign metadata %#x", candidate))
		}

		r.set(candidate)
		_ = r.engine.RegWrite(unicorn.RegX1, 0)
	case "swift_getWitnessTable":
		value := r.profile.SwiftWitnesses[profileformat.WitnessKey(r.arg(0), r.arg(1))]
		if value == 0 {
			panic(fmt.Sprintf("unprepared witness descriptor %#x for metadata %#x", r.arg(0), r.arg(1)))
		}

		r.set(value)
	case "$sSD17dictionaryLiteralSDyxq_Gx_q_td_tcfC":
		if r.read64(r.arg(0)+16) != 0 {
			panic("nonempty Swift dictionary literal unsupported")
		}

		r.set(r.profile.Symbols["_swiftEmptyDictionarySingleton"])
	case "swift_defaultActor_initialize":
		// Serial host scheduler: freshly allocated actor has an empty mailbox.
		// Enqueue/async entry points remain explicit unsupported services.
		r.objects[r.arg(0)] = &Object{Class: "HostSwiftActor", Properties: map[string]uint64{}}
	case "swift_getObjectType":
		r.set(r.read64(r.arg(0)))
	case "swift_getObjCClassFromMetadata":
		if r.classNames[r.arg(0)] != "" {
			break
		}

		if r.read64(r.arg(0)) == 0x305 {
			r.set(r.read64(r.arg(0) + 8))

			break
		}

		panic("unknown Objective-C class metadata")
	case "swift_getObjCClassMetadata":
		// Objective-C compatible class metadata uses the class address itself.
	case "swift_getTypeByMangledNameInContext2":
		value := r.profile.SwiftTypes[strconv.FormatUint(r.arg(0), 10)]
		if value == 0 || r.arg(2) != 0 || r.arg(3) != 0 {
			panic(fmt.Sprintf("unprepared Swift type %#x/%d", r.arg(0), r.arg(1)))
		}

		r.set(value)
	case "$sSS10FoundationE36_unconditionallyBridgeFromObjectiveCySSSo8NSStringCSgFZ":
		r.returnSwiftString(r.String(r.arg(0)))
	case "$sSS10FoundationE19_bridgeToObjectiveCSo8NSStringCyF":
		r.set(r.New("NSString", r.swiftString(r.arg(0), r.arg(1))))
	default:
		if strings.Contains(name, "NativeBoxIyLm8ELm8ELm8E") && strings.Contains(name, "initializeBufferWithCopyOfBuffer") {
			r.write64(r.arg(0), r.read64(r.arg(1)))

			return true
		}

		if strings.Contains(name, "BridgeObjectBox") && (strings.Contains(name, "initializeWithCopy") || strings.Contains(name, "assignWithCopy")) {
			r.write64(r.arg(0), r.read64(r.arg(1)))

			return true
		}

		if strings.Contains(name, "BridgeObjectBox") && strings.Contains(name, "destroy") {
			return true
		}

		if strings.Contains(name, "tuple_initializeWithCopy") || strings.Contains(name, "tuple_assignWithCopy") {
			size := r.read64(r.read64(r.arg(2)-8) + 64)
			if size > 1024 {
				panic("tuple copy exceeds budget")
			}

			data, err := r.engine.MemRead(r.arg(1), size)
			if err != nil {
				panic(err)
			}

			if err = r.MemWrite(r.arg(0), data); err != nil {
				panic(err)
			}

			return true
		}

		if strings.HasPrefix(name, "swift_memcpy") {
			var size, alignment uint64
			if _, err := fmt.Sscanf(name, "swift_memcpy%d_%d", &size, &alignment); err != nil || size > 1024 {
				panic("unsupported Swift copy size")
			}

			data, err := r.engine.MemRead(r.arg(1), size)
			if err != nil {
				panic(err)
			}

			if err = r.MemWrite(r.arg(0), data); err != nil {
				panic(err)
			}

			return true
		}

		if strings.Contains(name, "SwiftRetainableBox") && strings.Contains(name, "7destroy") {
			return true
		}

		if strings.Contains(name, "SwiftRetainableBox") && strings.Contains(name, "initializeBufferWithCopyOfBuffer") {
			r.write64(r.arg(0), r.read64(r.arg(1)))

			return true
		}

		if value := r.profile.SwiftGlobals[name]; value != 0 {
			r.set(value)

			return true
		}

		if (strings.HasPrefix(name, "$sSo17OS_dispatch_queueC8DispatchE") || strings.HasPrefix(name, "$s10Foundation3URLV13DirectoryHintO")) && strings.HasSuffix(name, "Owui") {
			metadata := r.arg(2)
			if r.read64(r.read64(metadata-8)+64) != 1 {
				panic("unexpected dispatch enum size")
			}

			if err := r.MemWrite(r.arg(0), []byte{byte(r.arg(1))}); err != nil {
				panic(err)
			}

			return true
		}

		if value := r.profile.SwiftAccessors[name]; value != 0 {
			r.set(value)
			_ = r.engine.RegWrite(unicorn.RegX1, 0)

			return true
		}

		return false
	}

	return true
}
func (r *Runtime) returnSwiftString(value string) {
	var words [16]byte
	if len(value) <= 15 {
		copy(words[:], value)

		words[15] = 0xa0 | byte(len(value))
		if isASCII(value) {
			words[15] |= 0x40
		}

		r.set(binary.LittleEndian.Uint64(words[:8]))
		_ = r.engine.RegWrite(unicorn.RegX1, binary.LittleEndian.Uint64(words[8:]))

		return
	}
	// Immortal native UTF-8 uses an address biased by the 32-byte storage header.
	p := r.alloc(uint64(len(value) + 33))
	if err := r.MemWrite(p+32, append([]byte(value), 0)); err != nil {
		panic(err)
	}

	flags := uint64(len(value)) | 0x3000000000000000
	if isASCII(value) {
		flags |= 0xc000000000000000
	}

	r.set(flags)
	_ = r.engine.RegWrite(unicorn.RegX1, p|0x8000000000000000)
}
func isASCII(s string) bool {
	for _, b := range []byte(s) {
		if b >= utf8.RuneSelf {
			return false
		}
	}

	return true
}
func (r *Runtime) swiftString(first, second uint64) string {
	if second&(1<<61) != 0 {
		var b [16]byte

		binary.LittleEndian.PutUint64(b[:8], first)
		binary.LittleEndian.PutUint64(b[8:], second)

		return string(b[:(second>>56)&15])
	}

	ptr := second & 0x0fffffffffffffff
	if second&(1<<60) != 0 {
		return r.String(ptr)
	}

	if second&(1<<63) == 0 {
		panic("unsupported mortal Swift string")
	}

	n := first & 0x0000ffffffffffff
	if n > 1<<20 {
		panic("Swift string exceeds runtime limit")
	}

	data, err := r.engine.MemRead(ptr+32, n)
	if err != nil {
		panic(err)
	}

	return string(data)
}

func (r *Runtime) mutableSwiftRequest() uint64 {
	self, _ := r.engine.RegRead(unicorn.RegX0 + 20)
	source := r.read64(self)

	object := r.objects[source]
	if object == nil {
		panic("invalid Swift request storage")
	}

	copied := r.New(object.Class, object.Value)

	if c := r.classes[object.Class]; c != nil {
		data, err := r.engine.MemRead(source, c.Size)
		if err != nil {
			panic(err)
		}

		if err = r.MemWrite(copied, data); err != nil {
			panic(err)
		}
	}

	for k, v := range object.Properties {
		r.objects[copied].Properties[k] = v
	}

	if p := object.Properties["allHTTPHeaderFields"]; p != 0 {
		r.objects[copied].Properties["allHTTPHeaderFields"] = r.foundationCopy(p, true)
	}

	r.write64(self, copied)

	return copied
}
