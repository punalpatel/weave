package main

import (
	"net"
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/weaveworks/go-odp/odp"

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
	flowsDesc           *prometheus.Desc
	bytesDesc           *prometheus.Desc
	packetsDesc         *prometheus.Desc
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
		flowsDesc: prometheus.NewDesc(
			"weave_flows",
			"Number of FastDP flows.",
			[]string{},
			prometheus.Labels{},
		),
		packetsDesc: prometheus.NewDesc(
			"weave_packets_total",
			"Number of packets transferred.",
			[]string{"flow"},
			prometheus.Labels{},
		),
		bytesDesc: prometheus.NewDesc(
			"weave_bytes_total",
			"Number of bytes transferred.",
			[]string{"flow"},
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
	uint64Counter := func(desc *prometheus.Desc, val uint64, labels ...string) {
		ch <- prometheus.MustNewConstMetric(desc, prometheus.CounterValue, float64(val), labels...)
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

	flowTag := func(f weave.FlowStatus) string {
		for _, k := range f.FlowKeys {
			if fk, ok := k.(odp.EthernetFlowKey); ok {
				ek := fk.Key() // TODO: worry about the Mask
				src := net.HardwareAddr(ek.EthSrc[:]).String()
				dst := net.HardwareAddr(ek.EthDst[:]).String()
				return src + "->" + dst
			}
		}
		return "other"
	}

	flows := 0
	var totalPackets, totalBytes uint64
	if diagMap, ok := routerStatus.OverlayDiagnostics.(map[string]interface{}); ok {
		if fastDPEntry, ok := diagMap["fastdp"]; ok {
			if fastDPStatus, ok := fastDPEntry.(weave.FastDPStatus); ok {
				flows = len(fastDPStatus.Flows)
				for _, flow := range fastDPStatus.Flows {
					tag := flowTag(flow)
					uint64Counter(m.packetsDesc, flow.Packets, tag)
					uint64Counter(m.bytesDesc, flow.Bytes, tag)
					totalPackets += flow.Packets
					totalBytes += flow.Bytes
				}
			}
		}
	}
	intMetric(m.flowsDesc, flows)
	uint64Counter(m.bytesDesc, totalBytes, "total")
	uint64Counter(m.packetsDesc, totalPackets, "total")

	ipamStatus := ipam.NewStatus(m.allocator, address.CIDR{})
	intMetric(m.ipsDesc, ipamStatus.RangeNumIPs, "total")
	intMetric(m.ipsDesc, ipamStatus.ActiveIPs, "local-used")

	nsStatus := nameserver.NewNSStatus(m.ns)
	intMetric(m.dnsDesc, countDNSEntries(nsStatus.Entries), "total")
	intMetric(m.dnsDesc, countDNSEntriesForPeer(m.router.Ourself.Name.String(), nsStatus.Entries), "local")
}

func (m *metrics) Describe(ch chan<- *prometheus.Desc) {
	ch <- m.connectionCountDesc
	ch <- m.flowsDesc
	ch <- m.bytesDesc
	ch <- m.packetsDesc
	ch <- m.ipsDesc
	ch <- m.dnsDesc
}
