package main

import (
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/weaveworks/weave/ipam"
	"github.com/weaveworks/weave/nameserver"
	"github.com/weaveworks/weave/net/address"
	weave "github.com/weaveworks/weave/router"
)

func metricsHandler(router *weave.NetworkRouter, allocator *ipam.Allocator, ns *nameserver.Nameserver) http.Handler {
	reg := prometheus.NewRegistry()
	reg.MustRegister(prometheus.NewProcessCollector(os.Getpid(), ""))
	reg.MustRegister(newMetrics(router, allocator, ns))
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
}

type metrics struct {
	router              *weave.NetworkRouter
	allocator           *ipam.Allocator
	ns                  *nameserver.Nameserver
	connectionCountDesc *prometheus.Desc
	ipsDesc             *prometheus.Desc
	dnsDesc             *prometheus.Desc
}

func newMetrics(router *weave.NetworkRouter, allocator *ipam.Allocator, ns *nameserver.Nameserver) *metrics {
	return &metrics{
		router:    router,
		allocator: allocator,
		ns:        ns,
		connectionCountDesc: prometheus.NewDesc(
			"weave_connections",
			"Number of peer-to-peer connections.",
			[]string{"state"},
			prometheus.Labels{},
		),
		ipsDesc: prometheus.NewDesc(
			"weave_ips",
			"Number of IP addresses.",
			[]string{"state"},
			prometheus.Labels{},
		),
		dnsDesc: prometheus.NewDesc(
			"weave_dns_entries",
			"Number of DNS entries.",
			[]string{"state"},
			prometheus.Labels{},
		),
	}
}

func (m *metrics) Collect(ch chan<- prometheus.Metric) {
	intMetric := func(desc *prometheus.Desc, val int, labels ...string) {
		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, float64(val), labels...)
	}

	routerStatus := weave.NewNetworkRouterStatus(m.router)

	established := 0
	for _, conn := range routerStatus.Connections {
		if conn.State == "established" {
			established++
		}
	}

	intMetric(m.connectionCountDesc, len(routerStatus.Connections)-established, "non-established")
	intMetric(m.connectionCountDesc, established, "established")

	ipamStatus := ipam.NewStatus(m.allocator, address.CIDR{})
	intMetric(m.ipsDesc, ipamStatus.RangeNumIPs, "total")
	intMetric(m.ipsDesc, ipamStatus.ActiveIPs, "local-used")

	nsStatus := nameserver.NewNSStatus(m.ns)
	intMetric(m.dnsDesc, countDNSEntries(nsStatus.Entries), "total")
	intMetric(m.dnsDesc, countDNSEntriesForPeer(m.router.Ourself.Name.String(), nsStatus.Entries), "local")
}

func (m *metrics) Describe(ch chan<- *prometheus.Desc) {
	ch <- m.connectionCountDesc
	ch <- m.ipsDesc
	ch <- m.dnsDesc
}
