package cmd

import (
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/liuchamp/gk/generator"
)

// initCmd represents the init command
var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Add new Function to service",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			logrus.Error("You must provide the service name")
			return
		}
		if viper.GetString("gk_transport") == "" {
			viper.Set("gk_transport", viper.GetString("default_transport"))
		}
		gen := generator.NewServiceUpdateGenerator()
		err := gen.Generate(args[0])
		if err != nil {
			logrus.Error(err, args)
			return
		}
	},
}

func init() {
	RootCmd.AddCommand(updateCmd)
	updateCmd.Flags().StringP("transport", "t", "", "Specify the transport you want to initiate for the service")
	viper.BindPFlag("gk_transport", initCmd.Flags().Lookup("transport"))

}
