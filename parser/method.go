package parser

import (
	"github.com/Sirupsen/logrus"
	template "github.com/yiv/gk/templates"
	"go/format"
)

type Method struct {
	Comment    string
	Name       string
	Struct     NamedTypeValue
	Body       string
	Parameters []NamedTypeValue
	Results    []NamedTypeValue
}

func NewMethod(name string, str NamedTypeValue, body string, parameters, results []NamedTypeValue) Method {
	return Method{
		Name:       name,
		Comment:    "",
		Struct:     str,
		Body:       body,
		Parameters: parameters,
		Results:    results,
	}
}

func NewMethodWithComment(name string, comment string, str NamedTypeValue, body string, parameters, results []NamedTypeValue) Method {
	m := NewMethod(name, str, body, parameters, results)
	m.Comment = prepareComments(comment)
	return m
}

func (m *Method) String() string {
	str := ""
	if m.Struct.Name != "" {
		s, err := template.NewEngine().ExecuteString("{{template \"struct_function\" .}}", m)
		if err != nil {
			logrus.Panic(err)
		}
		str = s
	} else {
		s, err := template.NewEngine().ExecuteString("{{template \"func\" .}}", m)
		if err != nil {
			logrus.Panic(err)
		}
		str = s
	}
	dt, err := format.Source([]byte(str))
	if err != nil {
		logrus.Panic(err)
	}
	return string(dt)
}
