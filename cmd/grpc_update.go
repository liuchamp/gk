package cmd

import (
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/liuchamp/gk/generator"
)

// grpcCmd represents the grpc command
var grpcUpdateCmd = &cobra.Command{
	Use:   "grpc",
	Short: "Update grpc transport after creating the protobuf",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			logrus.Error("You must provide the service name")
			return
		}
		g := generator.NewGRPCUpdateGenerator()
		err := g.Generate(args[0])
		if err != nil {
			logrus.Error(err)
			return
		}
	},
}

func init() {
	updateCmd.AddCommand(grpcUpdateCmd)
}
