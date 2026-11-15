package egress

import (
	"fmt"
	"os/exec"
	"strings"
)

// Mark is the fwmark the proxy puts on its upstream sockets; the firewall
// lets marked traffic out. Setting a mark needs CAP_NET_ADMIN, which job
// containers do not have.
const Mark = 0x5155

// Proxy ports on the instance.
const (
	HTTPPort = 15080
	TLSPort  = 15443
)

// Chains the firewall owns; the jumps into them are installed once, their
// contents are replaced per job.
const (
	chainOut = "QUILL-EGRESS-OUT" // filter OUTPUT: host + host-network jobs
	chainFwd = "QUILL-EGRESS-FWD" // filter DOCKER-USER: bridge-network containers
	chainIn  = "QUILL-EGRESS-IN"  // filter INPUT: keep the proxy ports private
	chainNat = "QUILL-EGRESS-NAT" // nat OUTPUT + PREROUTING: redirect 80/443
)

// Firewall installs the egress rules with iptables/ip6tables.
//
// Limits (recorded in DECISIONS.md): job containers get the Docker socket,
// so a workflow that deliberately starts a privileged container can lift
// these rules — they stop compromised dependencies and stray tooling, not a
// hostile workflow author. DNS (port 53) stays open, as does anything a
// client reaches through an allowed host (CDN domain fronting).
type Firewall struct {
	// Run executes one iptables command (tests record them).
	Run func(bin string, args ...string) error
	// V4/V6 are the binaries (iptables-nft / iptables-legacy, see Detect).
	V4, V6 string
}

type family struct {
	bin   string
	local []string // destinations that never leave the machine / docker nets
	v6    bool
}

func (f *Firewall) families() []family {
	fams := []family{{bin: f.V4, local: []string{"127.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}}}
	if f.V6 != "" {
		fams = append(fams, family{bin: f.V6, local: []string{"::1/128", "fe80::/10", "fc00::/7"}, v6: true})
	}
	return fams
}

// Init creates the chains and jumps (idempotent) and guards the proxy ports
// against traffic from outside the machine. Rules start in allow-all mode.
func (f *Firewall) Init() error {
	for _, fam := range f.families() {
		for _, c := range [][2]string{{"filter", chainOut}, {"filter", chainFwd}, {"filter", chainIn}, {"nat", chainNat}} {
			_ = f.Run(fam.bin, "-t", c[0], "-N", c[1]) // exists = fine
			if err := f.Run(fam.bin, "-t", c[0], "-F", c[1]); err != nil {
				if fam.v6 {
					f.V6 = "" // no usable IPv6 tables: v4 only
					break
				}
				return fmt.Errorf("%s: chain %s: %w", fam.bin, c[1], err)
			}
		}
		if fam.v6 && f.V6 == "" {
			continue
		}
		for _, j := range [][3]string{
			{"filter", "OUTPUT", chainOut},
			{"filter", "DOCKER-USER", chainFwd},
			{"filter", "INPUT", chainIn},
			{"nat", "OUTPUT", chainNat},
			{"nat", "PREROUTING", chainNat},
		} {
			if f.Run(fam.bin, "-t", j[0], "-C", j[1], "-j", j[2]) == nil {
				continue
			}
			if err := f.Run(fam.bin, "-t", j[0], "-I", j[1], "1", "-j", j[2]); err != nil {
				if j[1] == "DOCKER-USER" && fam.v6 {
					continue // docker without IPv6 has no v6 DOCKER-USER
				}
				return fmt.Errorf("%s: jump %s -> %s: %w", fam.bin, j[1], j[2], err)
			}
		}
		// The proxy listens on all addresses (redirected bridge traffic
		// arrives on the bridge address); only local and docker sources.
		in := [][]string{{"-i", "lo", "-j", "RETURN"}}
		for _, l := range fam.local {
			in = append(in, []string{"-s", l, "-j", "RETURN"})
		}
		in = append(in, []string{"-p", "tcp", "-m", "multiport", "--dports", fmt.Sprintf("%d,%d", HTTPPort, TLSPort), "-j", "DROP"})
		for _, r := range in {
			if err := f.Run(fam.bin, append([]string{"-t", "filter", "-A", chainIn}, r...)...); err != nil {
				return fmt.Errorf("%s: %w", fam.bin, err)
			}
		}
	}
	return nil
}

// Apply switches between allow-all (empty chains) and enforcement.
func (f *Firewall) Apply(enforce bool) error {
	for _, fam := range f.families() {
		for _, c := range [][2]string{{"filter", chainOut}, {"filter", chainFwd}, {"nat", chainNat}} {
			if err := f.Run(fam.bin, "-t", c[0], "-F", c[1]); err != nil {
				return fmt.Errorf("%s: flush %s: %w", fam.bin, c[1], err)
			}
		}
		if !enforce {
			continue
		}
		for _, r := range rules(fam) {
			if err := f.Run(fam.bin, r...); err != nil {
				return fmt.Errorf("%s %s: %w", fam.bin, strings.Join(r, " "), err)
			}
		}
	}
	return nil
}

// rules are the enforcement rules of one address family.
func rules(fam family) [][]string {
	mark := fmt.Sprintf("0x%x", Mark)
	reject := []string{"-j", "REJECT"}
	linkLocal := "169.254.0.0/16"
	if fam.v6 {
		linkLocal = "fe80::/10"
	}
	var r [][]string
	add := func(table, chain string, spec ...string) {
		r = append(r, append([]string{"-t", table, "-A", chain}, spec...))
	}
	// nat: everything HTTP(S) bound for the outside goes to the proxy.
	add("nat", chainNat, "-m", "mark", "--mark", mark, "-j", "RETURN")
	add("nat", chainNat, "-m", "addrtype", "--dst-type", "LOCAL", "-j", "RETURN")
	for _, l := range fam.local {
		add("nat", chainNat, "-d", l, "-j", "RETURN")
	}
	if !fam.v6 {
		add("nat", chainNat, "-d", linkLocal, "-j", "RETURN")
	}
	add("nat", chainNat, "-p", "tcp", "--dport", "80", "-j", "REDIRECT", "--to-ports", fmt.Sprint(HTTPPort))
	add("nat", chainNat, "-p", "tcp", "--dport", "443", "-j", "REDIRECT", "--to-ports", fmt.Sprint(TLSPort))

	// filter OUTPUT (the host, dockerd's pulls, host-network jobs).
	add("filter", chainOut, "-o", "lo", "-j", "RETURN")
	if !fam.v6 {
		add("filter", chainOut, "-d", linkLocal, "-j", "DROP") // metadata, even for the proxy
	}
	add("filter", chainOut, "-m", "mark", "--mark", mark, "-j", "RETURN")
	add("filter", chainOut, "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "RETURN")
	add("filter", chainOut, "-p", "udp", "--dport", "53", "-j", "RETURN")
	add("filter", chainOut, "-p", "tcp", "--dport", "53", "-j", "RETURN")
	if fam.v6 {
		add("filter", chainOut, "-p", "ipv6-icmp", "-j", "RETURN") // neighbour discovery
		add("filter", chainOut, "-p", "udp", "--dport", "546:547", "-j", "RETURN")
	} else {
		add("filter", chainOut, "-p", "udp", "--dport", "67:68", "-j", "RETURN")
		add("filter", chainOut, "-p", "udp", "--dport", "123", "-j", "RETURN")
	}
	for _, l := range fam.local {
		add("filter", chainOut, "-d", l, "-j", "RETURN")
	}
	add("filter", chainOut, reject...)

	// filter DOCKER-USER (containers on bridge networks; redirected
	// connections go through INPUT instead and never reach here).
	add("filter", chainFwd, "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "RETURN")
	add("filter", chainFwd, "-p", "udp", "--dport", "53", "-j", "RETURN")
	add("filter", chainFwd, "-p", "tcp", "--dport", "53", "-j", "RETURN")
	for _, l := range fam.local {
		add("filter", chainFwd, "-d", l, "-j", "RETURN")
	}
	add("filter", chainFwd, reject...)
	return r
}

// ExecRun runs an iptables binary.
func ExecRun(bin string, args ...string) error {
	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Detect picks the iptables backend Docker on the host uses (nft or
// legacy): rules in the other backend would live in separate tables. The
// variant that already holds Docker's DOCKER-USER chain wins.
func Detect(run func(bin string, args ...string) error) (v4, v6 string, err error) {
	for _, b := range []string{"nft", "legacy"} {
		if run("iptables-"+b, "-t", "filter", "-S", "DOCKER-USER") == nil {
			return "iptables-" + b, "ip6tables-" + b, nil
		}
	}
	return "", "", fmt.Errorf("egress: no iptables backend with Docker's DOCKER-USER chain (is Docker running?)")
}
