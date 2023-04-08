package util

func IfEmpty(value, fallback string) string {
	if value == "" {
		return fallback
	}

	return value
}
