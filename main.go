package main

import (
	"github.com/sirupsen/logrus"
	"github.com/spf13/afero"
	"github.com/spf13/viper"
	"github.com/yiv/gk/cmd"
	"github.com/yiv/gk/utils"
	"os"
	"strings"
)

func main() {
	logrus.SetReportCaller(false)
	viper.AutomaticEnv()
	gosrc := utils.GetGOPATH() + afero.FilePathSeparator + "src" + afero.FilePathSeparator
	pwd, err := os.Getwd()
	if err != nil {
		logrus.Error(err)
		return
	}
	if !strings.HasPrefix(pwd, gosrc) {
		logrus.Errorf("The project must be in the $GOPATH/src (%s) folder for the generator to work.", gosrc)
		return
	}
	cmd.Execute()
}
