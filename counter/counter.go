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
	"text/tabwriter"
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
		time.Sleep(time.Millisecond * 100) //time.Duration(c.multiplier))
		c.count++
		payload := NewIncrease(c.instanceid, c.count, c.multiplier)

		parcel := p2p.NewParcel(p2p.Broadcast, payload)
		c.network.ToNetwork.Send(parcel)
	}
}

func (c *Counter) Measure() {
	ticker := time.NewTicker(time.Second * 15)
	for range ticker.C {
		info := c.network.GetInfo()

		log.Println("Update")
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', tabwriter.Debug)
		fmt.Fprintln(tw, "Multiplier\tUseful\tTotal\tDown\tUp\tOur Count")
		fmt.Fprintf(tw, "%.2fx\t%d\t%d\t%s\t%s\t%d\n", c.multiplier, c.appMessagesUseful, c.appMessages, HRSpeed(info.Download), HRSpeed(info.Upload), c.count)

		tw.Flush()
		fmt.Println()
		tw = tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', tabwriter.Debug)
		fmt.Fprintln(tw, "AppID\tCount\tSkipped\tLast Active")
		c.dataMtx.Lock()
		for id, app := range c.data {
			if time.Since(app.Last) > time.Minute*5 {
				continue
			}
			nid := id
			if id == c.instanceid {
				nid = "me"
			}
			fmt.Fprintf(tw, "%s\t%d\t%d\t%s\n", nid, app.Count, app.Skipped, HRTime(time.Since(app.Last)))
		}
		c.dataMtx.Unlock()

		tw.Flush()
		fmt.Println()
		tw = tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', tabwriter.Debug)
		fmt.Fprintln(tw, "Hash\tMps Down\tMps Up\tDown\tUp\tCap\tDropped")
		for hash, p := range c.network.GetPeerMetrics() {
			fmt.Fprintf(tw, "%s\t%.2f\t%.2f\t%s\t%s\t%.2f\t%d\n", hash, p.MPSDown, p.MPSUp, HRSpeed(p.BPSDown), HRSpeed(p.BPSUp), p.Capacity, p.Dropped)
		}
		tw.Flush()
		fmt.Println("==========================================================")
		fmt.Println()
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

		id = id[:len(id)-1]

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
	existing, ok := c.data[hash]
	if ok {
		known = existing.Count
		skipped = existing.Skipped
	}

	if count > known {
		atomic.AddUint64(&c.appMessagesUseful, 1)
		if count > known+1 && ok {
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

func HRSpeed(speed float64) string {
	speed *= 8
	if speed >= 1e6 {
		return fmt.Sprintf("%.2f Mbit/s", speed/1e6)
	}
	if speed > 1e3 {
		return fmt.Sprintf("%.2f Kbit/s", speed/1e3)
	}
	return fmt.Sprintf("%.2f Bit/s", speed)
}

func HRTime(t time.Duration) string {
	if t < time.Second {
		return "<1s ago"
	}
	if t < time.Minute {
		return fmt.Sprintf("%ds ago", int(t.Seconds()))
	}
	return fmt.Sprintf("%dm ago", int(t.Minutes()))
}
