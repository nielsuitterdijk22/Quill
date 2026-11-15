package broker

import (
	"testing"

	"github.com/scaleway/scaleway-sdk-go/api/instance/v1"
	"github.com/scaleway/scaleway-sdk-go/scw"
)

// CI machines get a sized SBS root disk (the image default filled up).
func TestRootVolume(t *testing.T) {
	v := rootVolume(40)["0"]
	if v == nil || v.Size == nil || *v.Size != 40*scw.GB || v.VolumeType != instance.VolumeVolumeTypeSbsVolume || v.Boot == nil || !*v.Boot {
		t.Fatalf("root volume = %+v", v)
	}
}
