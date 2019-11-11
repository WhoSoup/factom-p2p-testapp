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

	appMessages       uint64
	appMessagesUseful uint64

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

	}
}

func (c *Counter) Measure() {
	ticker := time.NewTicker(time.Second * 15)
	for range ticker.C {
		var active, inactive int
		for _, b := range c.data {
			if time.Since(b.Last) < time.Minute {
				active++
			} else {
				inactive++
			}
		}

		info := c.network.GetInfo()

		log.Printf("[app] Peers[Active: %d, Inactive: %d, Connected: %d] Net[M/s: %.2f, KB/s: %.2f] App[Useful: %d, Total: %d]", active, inactive, info.Peers, info.Receiving+info.Sending, (info.Upload+info.Download)/1000, c.appMessagesUseful, c.appMessages)
	}
}

func (c *Counter) Handle(msg *p2p.Parcel) error {
	atomic.AddUint64(&c.appMessages, 1)

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
		atomic.AddUint64(&c.appMessagesUseful, 1)
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
