package profilegen

import (
	profileformat "github.com/majd/ipatool/v2/internal/store/profile/format"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Referenced Objective-C classes", func() {
	It("keeps referenced layouts, Store categories and superclasses while dropping unrelated classes", func() {
		classes := []profileformat.Class{
			{Name: "Unrelated"},
			{Name: "Category", Super: "Root", Methods: []profileformat.Method{{Selector: "storeMethod"}}},
			{Name: "Imported", Super: "Root", Size: 48, Ivars: map[string]uint64{"value": 16}},
			{Name: "Root", Size: 16},
		}
		actual := reachableClasses(classes, map[string]bool{"Imported": true, "Missing": true})
		Expect(actual).To(Equal([]profileformat.Class{classes[1], classes[2], classes[3]}))
	})
	It("terminates cyclic superclass chains deterministically", func() {
		classes := []profileformat.Class{{Name: "B", Super: "A"}, {Name: "A", Super: "B"}}
		Expect(reachableClasses(classes, map[string]bool{"B": true})).To(Equal([]profileformat.Class{classes[1], classes[0]}))
	})
})
