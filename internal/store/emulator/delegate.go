package emulator

import (
	"fmt"
	"time"
)

// The CLI owns interactive authentication. A caller delegate declines native UI
// so Apple's session can finish with an error and the existing CLI can interpret
// the server's 2FA, expired-token, or license response.
func (r *Runtime) interactiveDelegate() uint64 {
	const name = "IPAToolStoreDelegate"
	if r.classes[name] == nil {
		address := r.alloc(16)
		r.classes[name] = &Class{Name: name, Address: address, Super: "NSObject", Size: 16, Ivars: map[string]uint64{}}
		r.classNames[address] = name

		for index, selector := range []string{"AMSURLSession:task:handleAuthenticateRequest:completion:", "AMSURLSession:task:handleDialogRequest:completion:"} {
			code := int64(index + 1)
			method := selector
			block := r.hostBlock(func() {
				completion := r.arg(5)
				failure := r.New("NSError", map[string]uint64{"domain": r.New("NSString", "IPAToolStoreInteraction"), "code": r.New("NSNumber", code), "userInfo": r.New("NSDictionary", map[string]uint64{})})
				r.callback(completion, []uint64{0, failure}, func() { r.set(0) })
			})
			r.methods[methodKey(name, method, false)] = r.read64(block + 16)
		}
	}

	return r.New(name, nil)
}

func (r *Runtime) installStoreAMDAdapter() {
	install := func(address uint64, callback func()) {
		page := address &^ 4095
		if !r.pages[page] {
			if err := r.MemMap(page, 4096); err != nil {
				panic(err)
			}
		}

		if err := r.MemWrite(address, []byte{0xc0, 0x03, 0x5f, 0xd6}); err != nil {
			panic(err)
		}

		r.callbacks[address] = callback
	}

	install(0x180574a18, func() {
		r.set(r.New("HostXPCDictionary", map[string]uint64{}))
	})
	install(0x180553470, func() {
		dictionary := r.objects[r.arg(0)].Value.(map[string]uint64)

		length := r.arg(3)
		if length > 1<<20 {
			panic("XPC data exceeds runtime limit")
		}

		data, err := r.engine.MemRead(r.arg(2), length)
		if err != nil {
			panic(err)
		}

		dictionary[r.cstring(r.arg(1))] = r.New("NSData", data)
		r.set(0)
	})
	install(0x180553a00, func() {
		dictionary := r.objects[r.arg(0)].Value.(map[string]uint64)

		dataObject := r.objects[dictionary[r.cstring(r.arg(1))]]
		if dataObject == nil {
			if out := r.arg(2); out != 0 {
				r.write64(out, 0)
			}

			r.set(0)

			return
		}

		data := dataObject.Value.([]byte)
		pointer := r.alloc(uint64(max(len(data), 1)))

		if len(data) != 0 {
			if err := r.MemWrite(pointer, data); err != nil {
				panic(err)
			}
		}

		if out := r.arg(2); out != 0 {
			r.write64(out, uint64(len(data)))
		}

		r.set(pointer)
	})
	install(0x18054c6b8, func() { r.set(0) })
	install(0x180554c88, func() {
		connection := r.New("HostXPCConnection", r.cstring(r.arg(0)))
		r.objects[connection].Properties["targetQueue"] = r.arg(1)
		r.objects[connection].Properties["flags"] = r.arg(2)
		r.set(connection)
	})
	install(0x180554dac, func() {
		r.objects[r.arg(0)].Properties["eventHandler"] = r.arg(1)
		r.set(0)
	})
	install(0x1805550b4, func() { r.set(0) })
	install(0x180556440, func() {
		connection := r.objects[r.arg(0)]
		message := r.objects[r.arg(1)].Value.(map[string]uint64)

		dataObject := r.objects[message["data"]]
		if r.xpcSyncHandler == nil || dataObject == nil {
			panic("Store AMD XPC handler is unavailable")
		}

		reply, err := r.xpcSyncHandler(fmt.Sprint(connection.Value), dataObject.Value.([]byte))
		if err != nil {
			panic(err)
		}

		r.set(r.New("HostXPCDictionary", map[string]uint64{"data": r.New("NSData", reply)}))
	})
	install(0x184e50b38, func() {
		if token := r.arg(1); token != 0 {
			r.write64(token, 1)
		}

		r.set(0)
	})
	install(0x184e51370, func() {
		if changed := r.arg(1); changed != 0 {
			r.write64(changed, 0)
		}

		r.set(0)
	})
	install(0x18073bc1c, func() { r.set(0) })
	install(0x18071bec4, func() {
		now := time.Now()
		if value := r.arg(0); value != 0 {
			r.write64(value, uint64(now.Unix()))
			r.write64(value+8, uint64(now.Nanosecond()/1000))
		}

		r.set(0)
	})
}
