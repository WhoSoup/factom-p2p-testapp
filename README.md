# factom-p2p-testapp

A test application that simulates network traffic using the p2p2 library. It implements a simple counter app that increases a counter by one and then broadcasts an application message over the p2p network carrying the increased counter and a uniquely identifiable id (randomly generated on program start). Attached to the message is a junk (randomly generated) payload between 1 Byte and 4 KB. 

The app tracks counters for each message it receives, tracking all the talkative nodes on the network. Messages that increase a counter are considered **useful** and re-broadcast to the network, while messages that do not increase a counter are considered **not useful** and dropped. If a node hasn't updated its counter in the last 30 seconds, it counts as inactive.

At the default **multiplier** of 1, each node increases the counter every **100ms**, yielding 10 Messages/second. A node can increase the multiplier, which sends out a signal to all other nodes to also increase the multiplier. The formula is **100ms/multiplier**, so a multiplier of 100 yields one message every ms, or 1000 M/s. A multiplier of 500 yields 5000 M/s.

## Running

To take part in the official test, you can run it with the default parameters, which runs on port **8099** and grabs a [seed file from github](https://raw.githubusercontent.com/WhoSoup/factom-p2p-testapp/master/seed.txt).

The following parameters are available:
* **seed**: The url of the seed file. Default is `https://raw.githubusercontent.com/WhoSoup/factom-p2p-testapp/master/seed.txt`
* **bind**: (optional) a local interface to bind to. Default is blank to bind to any ip.
* **port**: The port that peers can connect to. Default is `8099`. If the port is not open to the public, you can still connect but no one can connect to you.
* **debug** (flag): Turn on debug logging for the p2p library if you are interested in more details. Can be pretty spammy and is not recommended.

## Output

Every 15 seconds, the app prints a message along the lines of:
> 2019/11/12 09:54:06 App[10000.00x] Peers[Active: 3, Inactive: 0, Connected: 2] Net[M/s: 63478.60, KB/s: 132075.35 (76320.69/55754.65)] App[Useful: 2208180, Total: 5837720]

* App: Contains the current multiplier
* Peers: List of nodes. Active are nodes that have increased their counter in the last 30 seconds, inactive are total known nodes. Connected is the amount of nodes the p2p system is connected to
* Net:
    * M/s is Messages/second (in and out) tracked by the p2p library. Includes application and p2p overhead messages
    * KB/s: The data rate of the p2p library. In parentheses it shows the download/upload rates. Does not include tcp overhead.
* App: Useful messages are ones that increase the counter. Total is all application messages received. Does not include p2p overhead messages.


## P2P Settings

The node's target is to peer to up to 8 nodes with a connection limit of 12. A broadcast message is sent to up to 4 peers.