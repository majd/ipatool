package emulator

func (r *Runtime) notificationMessage(receiver uint64, class, selector string, meta bool) bool {
	if meta && class == "NSNotificationCenter" && selector == "defaultCenter" {
		if r.notificationCenter == 0 {
			r.notificationCenter = r.New(class, nil)
		}

		r.set(r.notificationCenter)

		return true
	}

	if class == "NSNotification" {
		switch selector {
		case "name", "object", "userInfo":
			r.set(r.objects[receiver].Properties[selector])

			return true
		}

		return false
	}

	if meta || class != "NSNotificationCenter" {
		return false
	}

	switch selector {
	case "addObserver:selector:name:object:":
		observer := r.New("HostObserver", nil)
		o := r.objects[observer]
		o.Properties = map[string]uint64{"observer": r.arg(2), "selector": r.arg(3), "name": r.arg(4), "object": r.arg(5)}

		if r.observers == nil {
			r.observers = map[uint64]*Object{}
		}

		r.observers[observer] = o
		r.set(0)
	case "postNotificationName:object:userInfo:", "postNotificationName:object:":
		name, object := r.arg(2), r.arg(3)

		info := uint64(0)
		if selector == "postNotificationName:object:userInfo:" {
			info = r.arg(4)
		}

		notification := r.New("NSNotification", nil)

		r.objects[notification].Properties = map[string]uint64{"name": name, "object": object, "userInfo": info}
		for _, o := range r.observers {
			if p := o.Properties["name"]; p != 0 && r.String(p) != r.String(name) {
				continue
			}

			if p := o.Properties["object"]; p != 0 && p != object {
				continue
			}

			target, selector := o.Properties["observer"], r.cstring(o.Properties["selector"])
			r.pending = append(r.pending, r.hostBlock(func() { r.delegateCall(target, selector, []uint64{notification}, func() { r.set(0) }) }))
		}

		r.set(0)
	case "removeObserver:", "removeObserver:name:object:":
		for key, o := range r.observers {
			if key == r.arg(2) || o.Properties["observer"] == r.arg(2) {
				delete(r.observers, key)
			}
		}

		r.set(0)
	default:
		return false
	}

	return true
}
