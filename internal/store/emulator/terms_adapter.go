package emulator

import "github.com/majd/ipatool/v2/internal/store/terms"

// The caller handles account terms over the Store API. Reject only presentation
// of that page, allowing Execute to return the original finance response instead
// of falling back to AppKit. Other finance actions keep their native behavior.
func (r *Runtime) installTermsActionAdapter() {
	if r.termsActionAdapterInstalled {
		return
	}

	const class = "AMSFinanceActionResponse"

	const selector = "handleGotoURL:taskInfo:gotoType:accountURL:tidContinue:retryOnSuccess:"

	implementation := r.lookup(class, selector, true)
	block := r.hostBlock(func() {
		if !terms.IsPageURL(r.String(r.arg(2))) {
			arguments := make([]uint64, 8)
			for index := range arguments {
				arguments[index] = r.arg(index)
			}

			r.callbackFunction(implementation, arguments, func() {})

			return
		}

		failure := r.New("NSError", map[string]uint64{
			"domain":   r.New("NSString", "IPAToolStoreTerms"),
			"code":     r.New("NSNumber", int64(1)),
			"userInfo": r.New("NSDictionary", map[string]uint64{}),
		})
		r.callbackFunction(r.lookup("AMSPromise", "promiseWithError:", true),
			[]uint64{r.ClassAddress("AMSPromise"), r.selector("promiseWithError:"), failure}, func() {})
	})
	r.methods[methodKey(class, selector, true)] = r.read64(block + 16)
	r.termsActionAdapterInstalled = true
}
