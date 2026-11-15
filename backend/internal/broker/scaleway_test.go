package broker

import (
	"strings"
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

// CI machines boot with swap before Docker starts (2 GB RAM alone OOM-killed
// a large Go compile).
func TestCloudInitAddsSwap(t *testing.T) {
	ci := CloudInit(CloudInitOpts{APIURL: "https://api", WorkerToken: "t", WorkerImage: "img"})
	swap, docker := strings.Index(ci, "swapon /swapfile"), strings.Index(ci, "systemctl start docker")
	if swap < 0 || docker < 0 || swap > docker {
		t.Fatalf("swap not set up before docker:\n%s", ci)
	}
}
