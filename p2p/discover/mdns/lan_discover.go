package mdns

import (
	"net"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"

	"fmt"
	"github.com/bytom/event"
)

const (
	logModule            = "p2p/mdns"
	instanceName         = "bytomd"
	serviceName          = "lanDiscover"
	domainName           = "local"
	registerServiceCycle = 10 * time.Minute
	registerServiceDelay = 0 * time.Second
)

// LANPeerEvent represent LAN peer ip and port.
type LANPeerEvent struct {
	IP   []net.IP
	Port int
}

// mDNSProtocol mdns protocol interface.
type mDNSProtocol interface {
	registerService(instance string, service string, domain string, port int) error
	registerResolver(event chan LANPeerEvent, service string, domain string) error
	stopService()
	stopResolver()
}

// LANDiscover responsible for finding the related services registered LAN nodes.
type LANDiscover struct {
	protocol        mDNSProtocol
	resolving       uint32
	instance        string //instance name
	service         string //service name
	domain          string //domain name
	servicePort     int    //service port
	entries         chan LANPeerEvent
	eventDispatcher *event.Dispatcher
	quite           chan struct{}
}

// NewLANDiscover create a new LAN node discover.
func NewLANDiscover(protocol mDNSProtocol, port int) (*LANDiscover, error) {
	ld := &LANDiscover{
		protocol:        protocol,
		instance:        instanceName,
		service:         serviceName,
		domain:          domainName,
		servicePort:     port,
		entries:         make(chan LANPeerEvent, 1024),
		eventDispatcher: event.NewDispatcher(),
		quite:           make(chan struct{}),
	}
	// register service
	go ld.registerServiceRoutine()
	go ld.getLANPeerLoop()
	return ld, nil
}

// Stop stop LAN discover.
func (ld *LANDiscover) Stop() {
	close(ld.quite)
	ld.protocol.stopService()
	ld.protocol.stopResolver()
	ld.eventDispatcher.Stop()
}

// Subscribe used to subscribe for LANPeerEvent.
func (ld *LANDiscover) Subscribe() (*event.Subscription, error) {
	//subscribe LANPeerEvent.
	sub, err := ld.eventDispatcher.Subscribe(LANPeerEvent{})
	if err != nil {
		return nil, err
	}

	//need to register the parser once.
	if atomic.CompareAndSwapUint32(&ld.resolving, 0, 1) {
		if err = ld.protocol.registerResolver(ld.entries, ld.service, ld.domain); err != nil {
			return nil, err
		}
	}

	return sub, nil
}

// register service routine, will be re-registered periodically
// for the stability of node discovery.
func (ld *LANDiscover) registerServiceRoutine() {
	time.Sleep(registerServiceDelay)
	err := ld.protocol.registerService(ld.instance, ld.service, ld.domain, ld.servicePort)
	if err != nil {
		log.WithFields(log.Fields{"module": logModule, "err": err}).Error("mdns service register error")
		return
	}

	ticker := time.NewTicker(registerServiceCycle)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			ld.protocol.stopService()
			if err := ld.protocol.registerService(ld.instance, ld.service, ld.domain, ld.servicePort); err != nil {
				log.WithFields(log.Fields{"module": logModule, "err": err}).Error("mdns service register error")
				return
			}
		case <-ld.quite:
			return
		}
	}
}

// obtain the lan peer event from the specific protocol
// and distribute it to the subscriber.
func (ld *LANDiscover) getLANPeerLoop() {
	for {
		select {
		case entry := <-ld.entries:
			fmt.Println("===", entry)
			if err := ld.eventDispatcher.Post(entry); err != nil {
				log.WithFields(log.Fields{"module": logModule, "err": err}).Error("event dispatch error")
			}
		case <-ld.quite:
			return
		}
	}
}
