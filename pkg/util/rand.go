package util

import (
	"math/rand"
)

func RandInt(min, max int) int {
	return rand.Intn(max-min) + min
}
