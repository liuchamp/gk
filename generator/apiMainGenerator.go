package generator

import (
	"fmt"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/yiv/gk/fs"
	template "github.com/yiv/gk/templates"
)

type ApiMainGenerator struct {
}

func NewApiMainGenerator() *ApiMainGenerator {
	return &ApiMainGenerator{}
}

func (sg *ApiMainGenerator) Generate() error {
	name := "gateway"

	te := template.NewEngine()
	defaultFs := fs.Get()
	path, err := te.ExecuteString(viper.GetString("gateway.path"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return err
	}
	fname, err := te.ExecuteString(viper.GetString("gateway.file_name"), map[string]string{
		"ServiceName": name,
	})
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

	logrus.Info(fmt.Sprintf("Generating api gateway main"))

	err = defaultFs.MkdirAll(path)
	if err != nil {
		return err
	}
	tmpl, err := te.Execute("main_api", nil)
	if err != nil {
		return err
	}
	err = defaultFs.WriteFile(sfile, tmpl, false)
	if err != nil {
		return err
	}
	return nil
}
