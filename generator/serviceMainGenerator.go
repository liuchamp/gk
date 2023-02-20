package generator

import (
	"fmt"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/liuchamp/gk/fs"
	template "github.com/liuchamp/gk/templates"
)

type ServiceMainGenerator struct {
}

func NewServiceMainGenerator() *ServiceMainGenerator {
	return &ServiceMainGenerator{}
}

func (sg *ServiceMainGenerator) Generate(name string) error {
	logrus.Info(fmt.Sprintf("Generating service main: %s", name))

	te := template.NewEngine()
	defaultFs := fs.Get()
	model := map[string]string{"ServiceName": name}
	path, err := te.ExecuteString(viper.GetString("cmd.path"), model)
	if err != nil {
		return err
	}
	fname, err := te.ExecuteString(viper.GetString("cmd.file_name"), model)
	if err != nil {
		return err
	}
	sfile := path + defaultFs.FilePathSeparator() + fname
	is, err := defaultFs.Exists(sfile)
	if err != nil {
		return err
	}
	if is {
		return nil
	}
	err = defaultFs.MkdirAll(path)
	if err != nil {
		return err
	}
	tmpl, err := te.Execute("main_svc", model)
	if err != nil {
		return err
	}
	err = defaultFs.WriteFile(sfile, tmpl, false)
	if err != nil {
		return err
	}
	return nil
}
