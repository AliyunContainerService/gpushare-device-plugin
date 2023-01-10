package nvidia

import "testing"

func Test_generateFakeDeviceID(t *testing.T) {
	realId := "read-id-abc"

	t.Logf("fakeId %s", *generateFakeDeviceID(realId, 0))
}
