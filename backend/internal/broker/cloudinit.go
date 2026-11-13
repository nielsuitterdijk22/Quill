package broker

import (
	"fmt"
	"strings"
)

// CloudInitOpts renders the ephemeral CI instance's user-data.
type CloudInitOpts struct {
	// APIURL is the api base URL the ciworker claims through.
	APIURL string
	// DispatchSecret is the shared HMAC secret (QUILL_PIPELINE_DISPATCH_SECRET).
	DispatchSecret string
	// WorkerImage is the registry image of the ciworker container.
	WorkerImage string
	// Zone of the instance (passed through so the worker can self-terminate).
	Zone string
	// InstanceTag tags the instance for the orchestrator's list calls.
	InstanceTag string
	// RegistryUser/RegistryPassword authenticate the worker image pull from
	// the private Container Registry (the same ImageRegistry credentials via
	// docker login).
	RegistryUser     string
	RegistryPassword string
	// SCWAccessKey/SCWSecretKey/SCWProjectID let the worker self-terminate
	// its own instance (the terminate action needs an authenticated call).
	SCWAccessKey string
	SCWSecretKey string
	SCWProjectID string
	// UbuntuImage overrides act's ubuntu-latest image
	// (QUILL_PIPELINE_UBUNTU_IMAGE); empty keeps the runner default.
	UbuntuImage string
}

// CloudInit builds the cloud-init script: Docker + the ciworker container,
// then instance shutdown when ciworker exits (exit = queue dry — even if
// self-termination fails, a powered-off instance stops costing instantly;
// the broker's TTL-killer deletes stopped leftovers).
//
// The dispatch secret travels in user-data: same trust domain as everywhere
// (terraform state carries it in `user_data`, exactly like the Forgejo VM's
// cloud-init carries the Zitadel masterkey — recorded precedent).
func CloudInit(o CloudInitOpts) string {
	var b strings.Builder
	b.WriteString("#cloud-config\n")
	b.WriteString("package_update: true\n")
	b.WriteString("packages: [docker.io, ca-certificates]\n")
	b.WriteString("write_files:\n")
	b.WriteString("  - path: /etc/quill-ci.env\n    permissions: \"0600\"\n    content: |\n")
	for _, kv := range [][2]string{
		{"QUILL_API_URL", o.APIURL},
		{"QUILL_PIPELINE_DISPATCH_SECRET", o.DispatchSecret},
		{"QUILL_CIW_ZONE", o.Zone},
		{"QUILL_CIW_WORKER_ID", "ciworker-on-host"},
		{"QUILL_CIW_SELF_TERMINATE", "true"},
		{"QUILL_PIPELINE_ACTION_CACHE", "/mnt/quill-act-cache"},
		{"QUILL_PIPELINE_UBUNTU_IMAGE", o.UbuntuImage},
		{"SCW_ACCESS_KEY", o.SCWAccessKey},
		{"SCW_SECRET_KEY", o.SCWSecretKey},
		{"SCW_DEFAULT_PROJECT_ID", o.SCWProjectID},
	} {
		if kv[1] == "" {
			continue
		}
		fmt.Fprintf(&b, "      %s=%s\n", kv[0], kv[1])
	}
	b.WriteString("runcmd:\n")
	// User workflow containers run arbitrary code: they must not be able to
	// read the instance user-data (which carries the secrets above) off the
	// metadata service. DOCKER-USER filters only container (forwarded)
	// traffic; the worker itself runs on the host network and keeps metadata
	// access for its own instance id.
	b.WriteString("  - systemctl start docker\n")
	b.WriteString("  - iptables -I DOCKER-USER -d 169.254.42.42 -j DROP\n")
	b.WriteString("  - iptables -I DOCKER-USER -d 169.254.169.254 -j DROP\n")
	b.WriteString("  - ip6tables -I DOCKER-USER -d fd00:42::42 -j DROP || true\n")
	if o.RegistryUser != "" {
		fmt.Fprintf(&b, "  - docker login rg.fr-par.scw.cloud -u %s -p %s\n", o.RegistryUser, o.RegistryPassword)
	}
	fmt.Fprintf(&b, "  - docker run --rm --name ciworker -u 0 --network host --env-file /etc/quill-ci.env -v /mnt:/mnt -v /var/run/docker.sock:/var/run/docker.sock %s /quill-ciworker\n", o.WorkerImage)
	b.WriteString("  - shutdown -h now\n")
	return b.String()
}
