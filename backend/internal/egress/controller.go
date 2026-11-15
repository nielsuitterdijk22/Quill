package egress

import (
	"fmt"
	"net"
)

// Controller is the worker side: one proxy and one firewall per instance,
// switched per job.
type Controller struct {
	proxy    *Proxy
	fw       *Firewall
	required []string
}

// Start detects the iptables backend, starts the proxy listeners and
// installs the (allow-all) chains. required are the always-allowed hosts.
func Start(required []string) (*Controller, error) {
	v4, v6, err := Detect(ExecRun)
	if err != nil {
		return nil, err
	}
	fw := &Firewall{Run: ExecRun, V4: v4, V6: v6}
	p := NewProxy(PublicDialer(MarkSocket))
	for _, l := range []struct {
		port  int
		serve func(net.Listener) error
	}{{HTTPPort, p.ServeHTTP}, {TLSPort, p.ServeTLS}} {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", l.port))
		if err != nil {
			return nil, fmt.Errorf("egress proxy: %w", err)
		}
		go func() { _ = l.serve(ln) }()
	}
	if err := fw.Init(); err != nil {
		return nil, err
	}
	return &Controller{proxy: p, fw: fw, required: required}, nil
}

// Apply enforces pol for the next job (nil = allow all: queue rows from
// before egress controls existed). onBlock receives every refused host.
func (c *Controller) Apply(pol *Policy, onBlock func(host string, port int)) error {
	if !pol.Enforced() {
		c.proxy.SetPolicy(true, nil, nil)
		return c.fw.Apply(false)
	}
	c.proxy.SetPolicy(false, append(append([]string{}, c.required...), pol.Hosts...), onBlock)
	return c.fw.Apply(true)
}
