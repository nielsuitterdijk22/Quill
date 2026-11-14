package broker

import (
	"fmt"
	"strings"
)

// CloudInitOpts renders the ephemeral CI instance's user-data.
type CloudInitOpts struct {
	// APIURL is the api base URL the ciworker claims through.
	APIURL string
	// WorkerToken is this instance's own credential (internal/workertoken):
	// one worker, one instance, expiring. Since 2026-11-14 it replaces the
	// platform-wide dispatch secret on the machine.
	WorkerToken string
	// WorkerImage is the registry image of the ciworker container.
	WorkerImage string
	// Zone of the instance.
	Zone string
	// InstanceTag tags the instance for the orchestrator's list calls.
	InstanceTag string
	// RegistryUser/RegistryPassword authenticate the worker image pull from
	// the private Container Registry. Use a pull-only key: anything on the
	// machine can read it.
	RegistryUser     string
	RegistryPassword string
	// UbuntuImage overrides act's ubuntu-latest image
	// (QUILL_PIPELINE_UBUNTU_IMAGE); empty keeps the runner default.
	UbuntuImage string
}

// CloudInit builds the cloud-init script: Docker + the ciworker container,
// then instance shutdown when ciworker exits.
//
// Isolation (2026-11-14): job containers get the host's Docker socket (the
// platform's own deploy builds images), so anything a job runs can become
// root on this machine. The machine therefore carries nothing that reaches
// beyond the one repository its worker serves: no dispatch secret (a worker
// token instead, which the api binds to the repo of its first claim), no
// cloud key (the api has the broker terminate the instance when the worker
// retires), only a pull-only registry key. The metadata service (which
// serves this user-data) is blocked for the whole machine before the worker
// starts — defence in depth, since a root-equivalent job could lift it.
func CloudInit(o CloudInitOpts) string {
	var b strings.Builder
	b.WriteString("#cloud-config\n")
	b.WriteString("package_update: true\n")
	b.WriteString("packages: [docker.io, ca-certificates]\n")
	b.WriteString("write_files:\n")
	b.WriteString("  - path: /etc/quill-ci.env\n    permissions: \"0600\"\n    content: |\n")
	for _, kv := range [][2]string{
		{"QUILL_API_URL", o.APIURL},
		{"QUILL_CIW_TOKEN", o.WorkerToken},
		{"QUILL_CIW_ZONE", o.Zone},
		{"QUILL_CIW_SELF_TERMINATE", "true"},
		{"QUILL_PIPELINE_ACTION_CACHE", "/mnt/quill-act-cache"},
		// A run builds and pushes images on a 2-vCPU instance (the platform's
		// own deploy: four images + terraform); 30m was too tight.
		{"QUILL_PIPELINE_TIMEOUT", "60m"},
		{"QUILL_PIPELINE_UBUNTU_IMAGE", o.UbuntuImage},
	} {
		if kv[1] == "" {
			continue
		}
		fmt.Fprintf(&b, "      %s=%s\n", kv[0], kv[1])
	}
	b.WriteString("runcmd:\n")
	b.WriteString("  - systemctl start docker\n")
	// Containers on bridge networks (DOCKER-USER) and everything on the host
	// network, including act's job containers (OUTPUT), lose the metadata
	// service.
	b.WriteString("  - iptables -I DOCKER-USER -d 169.254.42.42 -j DROP\n")
	b.WriteString("  - iptables -I DOCKER-USER -d 169.254.169.254 -j DROP\n")
	b.WriteString("  - ip6tables -I DOCKER-USER -d fd00:42::42 -j DROP || true\n")
	b.WriteString("  - iptables -I OUTPUT -d 169.254.42.42 -j DROP\n")
	b.WriteString("  - iptables -I OUTPUT -d 169.254.169.254 -j DROP\n")
	b.WriteString("  - ip6tables -I OUTPUT -d fd00:42::42 -j DROP || true\n")
	if o.RegistryUser != "" {
		fmt.Fprintf(&b, "  - docker login rg.fr-par.scw.cloud -u %s -p %s\n", o.RegistryUser, o.RegistryPassword)
	}
	fmt.Fprintf(&b, "  - docker run --rm --name ciworker -u 0 --network host --env-file /etc/quill-ci.env -v /mnt:/mnt -v /var/run/docker.sock:/var/run/docker.sock %s /quill-ciworker\n", o.WorkerImage)
	b.WriteString("  - shutdown -h now\n")
	return b.String()
}
