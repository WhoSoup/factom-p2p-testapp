package counter

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/whosoup/factom-p2p"
)

type Counter struct {
	seed, bind, port string
	debug            bool

	instanceid string
	count      uint64

	data    map[string]Info
	dataMtx sync.Mutex

	appMessages       uint64
	appMessagesUseful uint64

	multiplier     float64
	lastMultiplier time.Time

	network *p2p.Network
}

type Info struct {
	Count   uint64
	Skipped uint64
	Last    time.Time
}

func NewCounter(seed, bind, port string, debug bool) *Counter {
	c := new(Counter)
	c.seed = seed
	c.bind = bind
	c.port = port
	c.debug = debug
	buf := make([]byte, 8)
	rng.Read(buf)
	c.instanceid = fmt.Sprintf("App%x", buf)
	c.multiplier = 1
	c.data = make(map[string]Info)
	return c
}

func (c *Counter) Run() error {
	log.Printf("[app] InstanceID = %s\n", c.instanceid)

	networkID := p2p.NewNetworkID("Test App Network")

	conf := p2p.DefaultP2PConfiguration()
	conf.ListenPort = c.port
	conf.Network = networkID
	conf.NodeName = fmt.Sprintf("Test App Node %s", c.instanceid)
	conf.PeerRequestInterval = time.Second * 5
	conf.SeedURL = c.seed
	conf.Target = 8
	conf.Drop = 6
	conf.Fanout = 5
	conf.Max = 12
	conf.BindIP = c.bind

	network, err := p2p.NewNetwork(conf)
	if err != nil {
		return err
	}

	if c.debug {
		p2p.DebugServer(network)
		log.Println("[app] Debug server started on http://localhost:8070/")
	}

	c.network = network
	c.network.Run()

	go c.Drive()
	go c.Measure()
	go c.Keyboard()
	for msg := range c.network.FromNetwork {
		c.Handle(msg)
	}
	return nil
}

func (c *Counter) Keyboard() {
	reader := bufio.NewReader(os.Stdin)
	for {
		in, _ := reader.ReadString('\n')
		in = strings.TrimSpace(in)
		if in == "" {
			continue
		}

		if time.Since(c.lastMultiplier) < time.Second*30 {
			log.Println("[app] can't change multiplier for another", c.lastMultiplier.Add(time.Second*30).Sub(time.Now()).Seconds(), "secs")
			continue
		}

		f, err := strconv.ParseFloat(in, 64)
		if err != nil {
			log.Println("invalid multiplier:", err)
			continue
		}

		log.Println("[app] Sending multiplier signal of", f)

		multimsg := NewMultiplier(c.instanceid, f)
		parcel := p2p.NewParcel(p2p.FullBroadcast, multimsg)
		c.network.ToNetwork.Send(parcel)
	}
}

func (c *Counter) Drive() {
	//ticker := time.NewTicker(time.Millisecond * 100)
	for {
		time.Sleep(time.Millisecond * 100 / time.Duration(c.multiplier))
		c.count++
		payload := NewIncrease(c.instanceid, c.count, c.multiplier)

		parcel := p2p.NewParcel(p2p.Broadcast, payload)
		c.network.ToNetwork.Send(parcel)
	}
}

func (c *Counter) Measure() {
	ticker := time.NewTicker(time.Second * 15)
	for range ticker.C {
		var active, inactive int
		c.dataMtx.Lock()
		for _, b := range c.data {
			if time.Since(b.Last) < time.Minute {
				active++
			} else {
				inactive++
			}
		}
		c.dataMtx.Unlock()

		info := c.network.GetInfo()

		log.Printf("App[%.2fx] Peers[Active: %d, Inactive: %d, Connected: %d] Net[M/s: %.2f, KB/s: %.2f (%.2f/%.2f)] App[Useful: %d, Total: %d]", c.multiplier, active, inactive, info.Peers, info.Receiving+info.Sending, (info.Upload+info.Download)/1000, info.Download/1000, info.Upload/1000, c.appMessagesUseful, c.appMessages)
		fmt.Printf("\tPeers\n")
		for hash, p := range c.network.GetPeerMetrics() {
			fmt.Printf("\t\t%25s Mps[%.2f/%.2f] KBs[%.2f/%.2f] Cap[%.2f]\n", hash, p.MPSDown, p.MPSUp, p.BPSDown/1000, p.BPSUp/1000, p.Capacity)
		}
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
	case MsgMultiplier:
		var multi float64
		if err := binary.Read(buf, binary.LittleEndian, &multi); err != nil {
			return fmt.Errorf("unable to read multiplier: %v", err)
		}

		if time.Since(c.lastMultiplier) > time.Second*30 && multi != c.multiplier {
			c.lastMultiplier = time.Now()
			c.multiplier = multi
			log.Printf("[app] multiplier updated to %.2f", multi)
			msg.Address = p2p.FullBroadcast
			c.network.ToNetwork.Send(msg)
		}
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
	c.dataMtx.Lock()
	defer c.dataMtx.Unlock()
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
