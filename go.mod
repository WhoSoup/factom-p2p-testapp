module github.com/whosoup/factom-p2p-testapp

go 1.13

require (
	github.com/gogo/protobuf v1.3.1
	github.com/golang/protobuf v1.3.2
	github.com/sirupsen/logrus v1.4.2
	github.com/spf13/cobra v0.0.5
	github.com/whosoup/factom-p2p v0.0.0-20191110100347-4056e9b2a323
)

replace github.com/whosoup/factom-p2p => ../factom-p2p
