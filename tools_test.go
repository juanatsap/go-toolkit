package toolkit

import "testing"

func TestTools_RandomString(t *testing.T) {
	var tools Tools
	const length = 10
	randomString := tools.RandomString(length)
	if len(randomString) != length {
		t.Errorf("Expected %d, got %d", length, len(randomString))
	}
}