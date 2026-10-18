package emulator

// The purchase factory configures request properties after encoder options.
// A caller subclass reapplies the explicit legacy-client signing policy before
// Apple's base encoder runs. No signature or authentication result is fabricated.
func (r *Runtime) configuredPurchaseEncoder(info, bag uint64) (uint64, error) {
	const className = "IPAToolPurchaseRequestEncoder"

	const selector = "requestByEncodingRequestAndReturnSigningErrors:parameters:"

	if r.classes[className] == nil {
		base := r.classes["AMSPurchaseRequestEncoder"]
		address := r.alloc(16)
		r.classes[className] = &Class{Name: className, Address: address, Super: base.Name, Size: base.Size, Ivars: map[string]uint64{}}
		r.classNames[address] = className
		implementation := r.lookup("AMSURLRequestEncoder", selector, false)
		block := r.hostBlock(func() {
			receiver, request, parameters := r.arg(0), r.arg(2), r.arg(3)
			r.delegateCall(receiver, "setAnisetteType:", []uint64{0}, func() {
				r.delegateCall(receiver, "setMescalType:", []uint64{0}, func() {
					r.delegateCall(request, "properties", nil, func() {
						properties := r.arg(0)
						r.delegateCall(properties, "setAnisetteType:", []uint64{0}, func() {
							r.delegateCall(properties, "setMescalType:", []uint64{0}, func() {
								r.delegateCall(properties, "setDisableLoadURLMetrics:", []uint64{1}, func() {
									r.delegateCall(receiver, "setDisableLoadURLMetrics:", []uint64{1}, func() {
										r.callbackFunction(implementation, []uint64{receiver, r.selector(selector), request, parameters}, func() {})
									})
								})
							})
						})
					})
				})
			})
		})
		r.methods[methodKey(className, selector, false)] = r.read64(block + 16)
	}

	return r.Invoke(r.New(className, nil), "initWithPurchaseInfo:bag:", info, bag)
}
