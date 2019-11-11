package counter

import (
	"bytes"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/whosoup/factom-p2p"
)

type Counter struct {
	seed, bind, port string

	instanceid string
	count      uint64

	data map[string]Info

	totalMsgReceived   uint64
	totalBytesReceived uint64
	totalMsgSent       uint64
	totalBytesSent     uint64

	bytesSent     uint64
	bytesReceived uint64
	msgSent       uint64
	msgReceived   uint64

	mps float64
	bps float64

	network *p2p.Network
}

type Info struct {
	Count   uint64
	Skipped uint64
	Last    time.Time
}

func NewCounter(seed, bind, port string) *Counter {
	c := new(Counter)
	c.seed = seed
	c.bind = bind
	c.port = port
	buf := make([]byte, 8)
	rng.Read(buf)
	c.instanceid = fmt.Sprintf("App%x", buf)

	c.data = make(map[string]Info)
	return c
}

func (c *Counter) Run() error {
	log.Printf("[app] InstanceID = %s\n", c.instanceid)

	networkID := p2p.NewNetworkID("Test App Network")

	conf := p2p.DefaultP2PConfiguration()
	conf.ListenPort = c.port
	conf.Network = networkID
	conf.NodeName = fmt.Sprintf("Test App Node %x", c.instanceid)
	conf.SeedURL = c.seed
	conf.Target = 16
	conf.Drop = 14
	conf.Fanout = 5
	conf.Max = 18

	network, err := p2p.NewNetwork(conf)
	if err != nil {
		return err
	}
	c.network = network
	c.network.Run()

	go c.Drive()
	go c.Measure()

	for msg := range c.network.FromNetwork {
		c.Handle(msg)
	}
	return nil
}

func (c *Counter) Drive() {
	ticker := time.NewTicker(time.Second)
	for range ticker.C {
		c.count++
		payload := NewIncrease(c.instanceid, c.count)

		parcel := p2p.NewParcel(p2p.Broadcast, payload)
		c.network.ToNetwork.Send(parcel)

		atomic.AddUint64(&c.totalMsgSent, 1)
		atomic.AddUint64(&c.msgSent, 1)
		atomic.AddUint64(&c.totalBytesSent, uint64(len(payload)))
		atomic.AddUint64(&c.bytesSent, uint64(len(payload)))
	}
}

func (c *Counter) Measure() {
	ticker := time.NewTicker(time.Second * 15)
	for range ticker.C {
		c.mps = (float64(c.msgReceived) + float64(c.msgSent)) / 15
		c.bps = (float64(c.bytesReceived) + float64(c.bytesSent)) / 15

		atomic.StoreUint64(&c.msgSent, 0)
		atomic.StoreUint64(&c.msgReceived, 0)
		atomic.StoreUint64(&c.bytesReceived, 0)
		atomic.StoreUint64(&c.bytesSent, 0)

		var active, inactive int
		for _, b := range c.data {
			if time.Since(b.Last) < time.Minute {
				active++
			} else {
				inactive++
			}
		}

		log.Printf("[app] Peers[%d Active, %d Inactive] App[M/s: %.2f, KB/s: %.2f] Total[M: %d, KB: %d]", active, inactive, c.mps, c.bps/1000, c.totalMsgReceived+c.totalMsgSent, (c.totalBytesReceived+c.totalBytesSent)/1000)
	}
}

func (c *Counter) Handle(msg *p2p.Parcel) error {
	atomic.AddUint64(&c.totalMsgReceived, 1)
	atomic.AddUint64(&c.msgReceived, 1)
	atomic.AddUint64(&c.totalBytesReceived, uint64(len(msg.Payload)))
	atomic.AddUint64(&c.bytesReceived, uint64(len(msg.Payload)))

	buf := new(IntBuffer)
	n, err := buf.ReadFrom(bytes.NewReader(msg.Payload))
	if err != nil {
		return fmt.Errorf("[app] Error reading payload: %v", err)
	}
	if n != int64(len(msg.Payload)) {
		return fmt.Errorf("[app] Error reading payload, only %d of %d bytes read", n, len(msg.Payload))
	}

	typ, err := buf.ReadUint32()
	if err != nil {
		return fmt.Errorf("unable to detect message type: %v", err)
	}

	switch typ {
	case MsgIncrease:
		count, err := buf.ReadUint64()
		if err != nil {
			return fmt.Errorf("unable to read count: %v", err)
		}

		id, err := buf.ReadString(Delim)
		if err != nil {
			return fmt.Errorf("unable to read id: %v", err)
		}

		if c.UpdateInfo(id, count) { // only send out when we updated, otherwise duplicate
			msg.Address = p2p.Broadcast
			c.network.ToNetwork.Send(msg)
		}
	default:
		return fmt.Errorf("invalid message type: %d", typ)
	}

	return nil
}

func (c *Counter) UpdateInfo(hash string, count uint64) bool {
	var known, skipped uint64
	if existing, ok := c.data[hash]; ok {
		known = existing.Count
		skipped = existing.Skipped
	}

	if count > known {
		if count > known+1 {
			skipped += count - known
		}
		known = count
		c.data[hash] = Info{
			Count:   known,
			Skipped: skipped,
			Last:    time.Now(),
		}
		return true
	}
	return false
}
