package emulator

import "time"

func (r *Runtime) frozenBagFromValues(values map[string]any) (uint64, error) {
	var err error

	builder := r.New("AMSBagFrozenDataSourceBuilder", nil)
	fields := map[string]uint64{"setData:": r.importValue(values), "setProfile:": r.New("NSString", "AMSPurchase"), "setProfileVersion:": r.New("NSString", "1"), "setExpirationDate:": r.New("NSDate", time.Now().Add(5*time.Minute))}

	for selector, value := range fields {
		if _, err = r.Invoke(builder, selector, value); err != nil {
			return 0, err
		}
	}

	return r.Invoke(builder, "createFrozenBag")
}
