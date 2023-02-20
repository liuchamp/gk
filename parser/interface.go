package parser

import (
	"github.com/sirupsen/logrus"
	template "github.com/liuchamp/gk/templates"
	"go/format"
)

type Interface struct {
	Name    string
	Comment string
	Methods []Method
}

func NewInterface(name string, methods []Method) Interface {
	return Interface{
		Name:    name,
		Comment: "",
		Methods: methods,
	}
}
func NewInterfaceWithComment(name string, comment string, methods []Method) Interface {
	i := NewInterface(name, methods)
	i.Comment = prepareComments(comment)
	return i
}

func (i *Interface) String() string {
	str, err := template.NewEngine().ExecuteString("{{template \"interface\" .}}", i)
	if err != nil {
		logrus.Panic(err)
	}
	dt, err := format.Source([]byte(str))
	if err != nil {
		logrus.Panic(err)
	}
	return string(dt)
}
