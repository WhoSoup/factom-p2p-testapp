package main

import (
	"fmt"
	"log"
	"os"

	log2 "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/whosoup/factom-p2p-testapp/counter"
)

var driver bool
var seed string
var bind string
var port string

func init() {
	rootCmd.Flags().StringVar(&seed, "seed", "http://localhost/tbd", "--seed=http://tbd")
	rootCmd.Flags().StringVar(&bind, "bind", "", "--bind=127.0.0.2")
	rootCmd.Flags().StringVar(&port, "port", "8099", "--port=8099")
}

var rootCmd = &cobra.Command{
	Use:   "testapp",
	Short: "Start the Test App",
	Args:  cobra.MaximumNArgs(0),
	Run: func(cmd *cobra.Command, args []string) {

		log2.SetLevel(log2.WarnLevel)
		counter := counter.NewCounter(seed, bind, port)

		if err := counter.Run(); err != nil {
			log.Fatal(err)
		}
	},
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
