package profilegen

import (
	"sort"

	profileformat "github.com/majd/ipatool/v2/internal/store/profile/format"
)

// reachableClasses retains Store implementations, referenced class identities,
// and their complete superclass chains. Imported classes without Store methods
// still need their layouts when the bridge allocates or inspects their objects.
func reachableClasses(classes []profileformat.Class, names map[string]bool) []profileformat.Class {
	byName := make(map[string]profileformat.Class, len(classes))
	for _, class := range classes {
		byName[class.Name] = class

		if len(class.Methods) != 0 {
			names[class.Name] = true
		}
	}

	kept := map[string]bool{}
	for name := range names {
		for name != "" && !kept[name] {
			class, ok := byName[name]
			if !ok {
				break
			}

			kept[name] = true
			name = class.Super
		}
	}

	result := make([]profileformat.Class, 0, len(kept))
	for name := range kept {
		result = append(result, byName[name])
	}

	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })

	return result
}
