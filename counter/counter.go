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

	"github.com/FactomProject/factomd/p2p"
)

type Counter struct {
	seed, bind, port string
	debug            bool

	instanceid string
	count      uint64

	data    map[string]Info
	dataMtx sync.Mutex

	appMessages        uint64
	appMessagesUseful  uint64
	appMessagesDropped uint64

	multiplier     float64
	lastMultiplier time.Time

	metrics chan interface{}

	network *p2p.Controller
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
func (c *Counter) StartFactomP2P() {
	p2p.MaxNumberIncomingConnections = 12
	p2p.NumberPeersToConnect = 8

	networkID := p2p.NetworkID(binary.BigEndian.Uint32(Sha([]byte("Test App Old Network"))[:4]))

	c.metrics = make(chan interface{}, p2p.StandardChannelSize)
	p2p.NetworkDeadline = 5 * time.Minute

	ci := p2p.ControllerInit{
		NodeName:                 fmt.Sprintf("Test App Node %s", c.instanceid),
		Port:                     c.port,
		PeersFile:                "C:\\work\\tmp\\p2pold-peers.json",
		Network:                  networkID,
		Exclusive:                false,
		ExclusiveIn:              false,
		SeedURL:                  c.seed,
		ConfigPeers:              "",
		CmdLinePeers:             "",
		ConnectionMetricsChannel: c.metrics,
	}
	p2pNetwork := new(p2p.Controller).Init(ci)
	p2pNetwork.StartNetwork()
	c.network = p2pNetwork
}

func (c *Counter) Run() error {
	log.Printf("[app] InstanceID = %s\n", c.instanceid)

	c.StartFactomP2P()

	go c.Drive()
	go c.Measure()
	go c.Keyboard()
	for msg := range c.network.FromNetwork {
		parc := msg.(p2p.Parcel)
		c.Handle(parc)
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

		if time.Since(c.lastMultiplier) < time.Second {
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

		parcels := p2p.ParcelsForPayload(p2p.CurrentNetwork, multimsg)
		for _, parcel := range parcels {
			if parcel.Header.Type != p2p.TypeMessagePart {
				parcel.Header.Type = p2p.TypeMessage
			}
			parcel.Header.TargetPeer = p2p.FullBroadcastFlag
			dropped := p2p.BlockFreeChannelSend(c.network.ToNetwork, parcel)
			atomic.AddUint64(&c.appMessagesDropped, uint64(dropped))
		}
	}
}

func (c *Counter) Drive() {
	//ticker := time.NewTicker(time.Millisecond * 100)
	for {
		time.Sleep(time.Millisecond * 100) // / time.Duration(c.multiplier))
		c.count++
		payload := NewIncrease(c.instanceid, c.count, c.multiplier)

		parcels := p2p.ParcelsForPayload(p2p.CurrentNetwork, payload)
		for _, parcel := range parcels {
			if parcel.Header.Type != p2p.TypeMessagePart {
				parcel.Header.Type = p2p.TypeMessage
			}
			parcel.Header.TargetPeer = p2p.FullBroadcastFlag
			dropped := p2p.BlockFreeChannelSend(c.network.ToNetwork, parcel)
			atomic.AddUint64(&c.appMessagesDropped, uint64(dropped))
		}
	}
}

func (c *Counter) Measure() {
	//ticker := time.NewTicker(time.Second )
	var prevmessages uint64
	for m := range c.metrics {
		info := m.(map[string]p2p.ConnectionMetrics)
		log.Println("Update", len(info))

		mps := float64((c.appMessages - prevmessages)) / 15
		prevmessages = c.appMessages

		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', tabwriter.Debug)
		fmt.Fprintln(tw, "Multiplier\tUseful\tTotal\tDropped\tDown\tUp\tOur Count\tAppMPS")
		fmt.Fprintf(tw, "%.2fx\t%d\t%d\t%d\t%s\t%s\t%d\t%f\n", c.multiplier, c.appMessagesUseful, c.appMessages, c.appMessagesDropped, "n/a", "n/a", c.count, mps)

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
		fmt.Fprintln(tw, "Hash\tMps Down\tMps Up\tDown\tUp")
		for s, i := range info {
			fmt.Fprintf(tw, "%s\t%f\t%f\t%s\t%s\n", s, i.NewMsgRecv, i.NewMsgSent, HRSpeed(i.NewBytesDown), HRSpeed(i.NewBytesUp))
		}
		tw.Flush()
		fmt.Println("=======================================================")

	}
}

func (c *Counter) Handle(msg p2p.Parcel) error {
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

		if time.Since(c.lastMultiplier) > time.Second && multi != c.multiplier {
			c.lastMultiplier = time.Now()
			c.multiplier = multi
			log.Printf("[app] multiplier updated to %.2f", multi)
			msg.Header.TargetPeer = p2p.FullBroadcastFlag
			dropped := p2p.BlockFreeChannelSend(c.network.ToNetwork, msg)
			atomic.AddUint64(&c.appMessagesDropped, uint64(dropped))
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
			msg.Header.TargetPeer = p2p.FullBroadcastFlag
			dropped := p2p.BlockFreeChannelSend(c.network.ToNetwork, msg)
			atomic.AddUint64(&c.appMessagesDropped, uint64(dropped))

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
