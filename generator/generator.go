package generator

import (
	"errors"
	"fmt"
	"github.com/yiv/gk/utils"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/yiv/gk/fs"
	"github.com/yiv/gk/parser"
	template "github.com/yiv/gk/templates"
)

var SUPPORTED_TRANSPORTS = []string{"http", "grpc", "thrift"}

func LoadServiceInterfaceFromFile(name string) (*parser.Interface, error) {
	logrus.Info("load interfaces from exist file for service ", name)
	te := template.NewEngine()
	defaultFs := fs.Get()
	path, err := te.ExecuteString(viper.GetString("service.path"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return nil, err
	}
	fname, err := te.ExecuteString(viper.GetString("service.file_name"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return nil, err
	}
	sfile := path + defaultFs.FilePathSeparator() + fname
	exist, err := defaultFs.Exists(sfile)
	if err != nil {
		return nil, err
	}
	iname, err := te.ExecuteString(viper.GetString("service.interface_name"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return nil, err
	}
	if !exist {
		return nil, errors.New(fmt.Sprintf("Service %s was not found", name))
	}
	p := parser.NewFileParser()
	s, err := defaultFs.ReadFile(sfile)
	if err != nil {
		return nil, err
	}
	f, err := p.Parse([]byte(s))
	if err != nil {
		return nil, err
	}

	var iface *parser.Interface
	for _, v := range f.Interfaces {
		if v.Name == iname {
			iface = &v
		}
	}
	if iface == nil {
		return nil, errors.New(fmt.Sprintf("Could not find the service interface in `%s`", sfile))
	}
	toKeep := []parser.Method{}
	for _, v := range iface.Methods {
		isOk := false
		for _, p := range v.Parameters {
			if p.Type == "context.Context" {
				isOk = true
				break
			}
		}
		if string(v.Name[0]) == strings.ToLower(string(v.Name[0])) {
			logrus.Warnf("The method '%s' is private and will be ignored", v.Name)
			continue
		}
		if len(v.Results) == 0 {
			logrus.Warnf("The method '%s' does not have any return value and will be ignored", v.Name)
			continue
		}
		if !isOk {
			logrus.Warnf("The method '%s' does not have a context and will be ignored", v.Name)
		}
		if isOk {
			toKeep = append(toKeep, v)
		}

	}
	iface.Methods = toKeep
	if len(iface.Methods) == 0 {
		return nil, errors.New("The service has no method please implement the interface methods")
	}
	return iface, nil
}
func IsProtoCompiled(name string) (yes bool, err error) {
	te := template.NewEngine()
	defaultFs := fs.Get()
	var (
		path, sfile string
		exist       bool
	)
	path, err = te.ExecuteString(viper.GetString("pb.path"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return false, err
	}
	sfile = path + defaultFs.FilePathSeparator() + utils.ToLowerSnakeCase(name) + ".pb.go"
	exist, err = defaultFs.Exists(sfile)
	if err != nil {
		return false, err
	}
	if !exist {
		logrus.Error("Not found: ", sfile)
		return false, errors.New("Could not find the compiled pb of the service")
	}
	return true, nil
}
