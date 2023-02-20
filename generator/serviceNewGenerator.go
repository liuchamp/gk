package generator

import (
	"fmt"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/liuchamp/gk/fs"
	"github.com/liuchamp/gk/parser"
	template "github.com/liuchamp/gk/templates"
)

type ServiceGenerator struct {
}

func NewServiceGenerator() *ServiceGenerator {
	return &ServiceGenerator{}
}

func (sg *ServiceGenerator) Generate(name string) error {
	{
		apiGen := NewApiMainGenerator()
		if err := apiGen.Generate(); err != nil {
			return err
		}
	}
	{
		mainGen := NewServiceMainGenerator()
		if err := mainGen.Generate(name); err != nil {
			return err
		}
	}
	logrus.Info(fmt.Sprintf("Generating service: %s", name))
	f := parser.NewFile()
	f.Package = fmt.Sprintf("%sservice", name)
	te := template.NewEngine()
	iname, err := te.ExecuteString(viper.GetString("service.interface_name"), map[string]string{
		"ServiceName": name,
	})
	logrus.Debug(fmt.Sprintf("Service interface name : %s", iname))
	if err != nil {
		return err
	}

	f.AliasType = []parser.NamedTypeValue{parser.NewNameType("Middleware", "func(Service) Service")}

	svcInterface := []parser.Interface{
		parser.NewInterfaceWithComment(iname, `
		Service describes a service that adds things together
		Implement yor service methods methods.
		e.x: FooToo(ctx context.Context,inParam string)(outParam string, err error)`, []parser.Method{
			parser.NewMethod("FooToo", parser.NamedTypeValue{}, "", []parser.NamedTypeValue{
				parser.NewNameType("ctx", "context.Context"),
				parser.NewNameType("inParam", "string"),
			}, []parser.NamedTypeValue{
				parser.NewNameType("outParam", "string"),
				parser.NewNameType("err", "error"),
			}),
		}),
	}
	f.Interfaces = svcInterface

	defaultFs := fs.Get()

	path, err := te.ExecuteString(viper.GetString("service.path"), map[string]string{
		"ServiceName": name,
	})
	logrus.Debug(fmt.Sprintf("Service path: %s", path))
	if err != nil {
		return err
	}
	b, err := defaultFs.Exists(path)
	if err != nil {
		return err
	}
	fname, err := te.ExecuteString(viper.GetString("service.file_name"), map[string]string{
		"ServiceName": name,
	})
	logrus.Debug(fmt.Sprintf("Service file name: %s", fname))
	if err != nil {
		return err
	}
	if b {
		logrus.Debug("Service folder already exists")
		return fs.NewDefaultFs(path).WriteFile(fname, f.String(), false)
	}
	err = defaultFs.MkdirAll(path)
	logrus.Debug(fmt.Sprintf("Creating folder structure : %s", path))
	if err != nil {
		return err
	}
	return fs.NewDefaultFs(path).WriteFile(fname, f.String(), false)
}
