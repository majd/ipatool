package emulator

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
