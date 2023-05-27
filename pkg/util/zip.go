package util

import "errors"

type Pair[T, U any] struct {
	First  T
	Second U
}

func Zip[T, U any](ts []T, us []U) ([]Pair[T, U], error) {
	if len(ts) != len(us) {
		return nil, errors.New("slices have different lengths")
	}

	pairs := make([]Pair[T, U], len(ts))
	for i := 0; i < len(ts); i++ {
		pairs[i] = Pair[T, U]{
			First:  ts[i],
			Second: us[i],
		}
	}

	return pairs, nil
}
