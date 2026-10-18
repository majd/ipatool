package emulator

import "strings"

// Ephemeral Accounts-framework values. No host account store or keychain writes.
func (r *Runtime) accountMessage(receiver uint64, class, selector string, meta bool) bool {
	if !meta && (class == "ACAccountStore" || class == "ACDAccountStore") {
		switch selector {
		case "accountTypeWithAccountTypeIdentifier:", "accountTypeWithAccountTypeIdentifier:error:":
			accountType := r.New("ACAccountType", nil)

			r.objects[accountType].Properties = map[string]uint64{"identifier": r.arg(2)}
			if selector == "accountTypeWithAccountTypeIdentifier:error:" && r.arg(3) != 0 {
				r.write64(r.arg(3), 0)
			}

			r.set(accountType)

			return true
		case "accountsWithAccountType:", "accountsWithAccountType:options:error:", "accountsWithAccountType:options:classification:error:":
			if selector == "accountsWithAccountType:options:error:" && r.arg(4) != 0 {
				r.write64(r.arg(4), 0)
			}

			if selector == "accountsWithAccountType:options:classification:error:" && r.arg(5) != 0 {
				r.write64(r.arg(5), 0)
			}

			r.set(r.New("NSArray", []uint64{}))

			return true
		case "accounts":
			r.set(r.New("NSArray", []uint64{}))

			return true
		case "accountWithIdentifier:":
			r.set(0)

			return true
		}
	}

	if meta || (!r.kindOf(class, "ACAccount") && class != "ACAccountType" && class != "ACAccountCredential") {
		return false
	}

	object := r.objects[receiver]

	switch selector {
	case "initWithIdentifier:description:":
		object.Properties["identifier"] = r.arg(2)
		object.Properties["accountTypeDescription"] = r.arg(3)
		r.set(receiver)
	case "initWithAccountType:":
		object.Properties["identifier"] = r.New("NSString", r.newUUIDString())
		object.Properties["accountType"] = r.arg(2)
		object.Properties["accountProperties"] = r.New("NSMutableDictionary", map[string]uint64{})
		r.set(receiver)
	case "initWithPassword:":
		object.Properties["password"] = r.arg(2)
		r.set(receiver)
	case "accountPropertyForKey:":
		fields := r.objects[object.Properties["accountProperties"]].Value.(map[string]uint64)
		r.set(fields[r.String(r.arg(2))])
	case "setAccountProperty:forKey:":
		fields := r.objects[object.Properties["accountProperties"]].Value.(map[string]uint64)
		key := r.String(r.arg(3))

		if r.arg(2) == 0 {
			delete(fields, key)
		} else {
			fields[key] = r.arg(2)
		}

		r.set(0)
	case "accountType", "identifier", "username", "credential", "accountProperties", "authenticated", "active", "owningBundleID", "accountDescription", "parentAccount", "isDirty":
		r.set(object.Properties[selector])
	default:
		if strings.HasPrefix(selector, "set") && strings.HasSuffix(selector, ":") && strings.Count(selector, ":") == 1 {
			key := selector[3 : len(selector)-1]
			if len(key) < 2 || key[1] < 'A' || key[1] > 'Z' {
				key = strings.ToLower(key[:1]) + key[1:]
			}

			object.Properties[key] = r.arg(2)
			r.set(0)

			return true
		}

		return false
	}

	return true
}
